package content

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"

	ccontent "github.com/containerd/containerd/v2/core/content"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/reference"
)

var _ Target = (*OCI)(nil)

// ErrDigestMismatch is returned by WriteBlob (and wrapped with details) when
// the content actually streamed from open() does not hash to the expected
// digest. Callers can retry: the final blob path is never touched on this
// error, so a fresh WriteBlob call will re-download cleanly.
var ErrDigestMismatch = errors.New("content: digest mismatch")

// ctxReader wraps an io.Reader so that Read starts returning ctx.Err() once
// ctx is done, instead of delegating to the underlying reader. This is what
// makes an in-flight WriteBlob copy (io.Copy driven, chunk by chunk) actually
// stop when its context is cancelled, rather than running to completion and
// only then being noticed. Checking before each delegated Read -- not via a
// second goroutine racing the real Read -- keeps this allocation-free and
// correct for readers with no cancellation hook of their own (v1.Layer's
// Compressed(), which this package doesn't control).
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

type OCI struct {
	root    string
	index   *ocispec.Index
	nameMap *sync.Map // map[string]ocispec.Descriptor
	sf      singleflight.Group

	// blobSem bounds the number of blob writes in flight at once across this
	// OCI's two write paths, WriteBlob and ociPusher.Push -- a hard, process
	// -wide ceiling regardless of how many images or errgroups are fanning
	// out concurrently above it. It lives here (rather than on store.Layout,
	// which embeds *OCI) so both write paths share it without either one
	// having to reach into the other's state, and so it is naturally scoped
	// per-store the same way sf already is: two Layouts in one process (every
	// test file) never share permits across roots. Acquired with
	// sem.Acquire(ctx, 1), which -- unlike errgroup.SetLimit -- returns
	// promptly on context cancellation instead of leaving a goroutine parked.
	blobSem *semaphore.Weighted

	// mu guards index, index.json on disk, and the annotation maps of
	// descriptors reachable from nameMap. It is a plain sync.Mutex, not an
	// RWMutex: LoadIndex always writes both o.index and o.nameMap, so there is
	// no pure-reader path that would benefit from a reader/writer split.
	//
	// Every exported method that touches index/nameMap/disk is a thin
	// lock-then-call-Locked-variant wrapper. The unexported *Locked methods
	// assume the caller already holds mu; this is what lets internal call
	// chains (e.g. ociPusher.Push's load-modify-save) re-enter safely without
	// calling Lock() twice, which would deadlock even without Walk's
	// self-referential-callback hazard (see Walk's doc comment).
	mu sync.Mutex
}

// OCIOption configures an OCI store at construction time.
type OCIOption func(*OCI)

// WithBlobConcurrency overrides consts.DefaultBlobConcurrency for this OCI's
// blobSem. n <= 0 is a no-op (keeps the default) rather than an error, so
// callers can pass a possibly-zero value unconditionally.
func WithBlobConcurrency(n int) OCIOption {
	return func(o *OCI) {
		if n > 0 {
			o.blobSem = semaphore.NewWeighted(int64(n))
		}
	}
}

func NewOCI(root string, opts ...OCIOption) (*OCI, error) {
	o := &OCI{
		root:    root,
		nameMap: &sync.Map{},
		blobSem: semaphore.NewWeighted(consts.DefaultBlobConcurrency),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o, nil
}

// AddIndex adds a descriptor to the index and updates it
//
//	The descriptor must use AnnotationRefName to identify itself
func (o *OCI) AddIndex(desc ocispec.Descriptor) error {
	// Pure validation/parsing -- doesn't touch shared state -- stays outside
	// the lock.
	if _, ok := desc.Annotations[ocispec.AnnotationRefName]; !ok {
		return fmt.Errorf("descriptor must contain a reference from the annotation: %s", ocispec.AnnotationRefName)
	}

	key, err := reference.Parse(desc.Annotations[ocispec.AnnotationRefName])
	if err != nil {
		return err
	}

	if strings.TrimSpace(key.String()) == "--" {
		return nil
	}

	var mapKey string
	switch key.(type) {
	case name.Digest:
		mapKey = fmt.Sprintf("%s-%s", key.Context().String(), desc.Annotations[consts.KindAnnotationName])
	case name.Tag:
		mapKey = fmt.Sprintf("%s-%s", key.String(), desc.Annotations[consts.KindAnnotationName])
	default:
		return nil
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	// Cheap win: skip the nameMap.Store + saveIndexLocked write entirely when
	// the stored descriptor is already byte-identical to desc. Per-artifact
	// SaveIndex writes are intentionally not batched/coalesced across an
	// entire sync (O(N^2) total bytes written as the index grows -- this
	// starts to matter around ~10k artifacts) because per-artifact writes
	// preserve crash resumability on slow air-gap links: a coalescing writer
	// is future work, not part of this change.
	if existing, ok := o.nameMap.Load(mapKey); ok {
		if descriptorsEqual(existing.(ocispec.Descriptor), desc) {
			return nil
		}
	}

	o.nameMap.Store(mapKey, desc)
	return o.saveIndexLocked()
}

// descriptorsEqual reports whether two descriptors are equal in every field
// that AddIndex's callers in this codebase populate: MediaType, Digest,
// Size, URLs, ArtifactType, Platform, Data, and Annotations (compared by
// contents, not map identity). It is deliberately conservative -- any field
// that could plausibly differ between calls is compared, not assumed equal.
func descriptorsEqual(a, b ocispec.Descriptor) bool {
	if a.MediaType != b.MediaType || a.Digest != b.Digest || a.Size != b.Size || a.ArtifactType != b.ArtifactType {
		return false
	}
	if !maps.Equal(a.Annotations, b.Annotations) {
		return false
	}
	if !slices.Equal(a.URLs, b.URLs) {
		return false
	}
	if !bytes.Equal(a.Data, b.Data) {
		return false
	}
	if (a.Platform == nil) != (b.Platform == nil) {
		return false
	}
	if a.Platform != nil {
		pa, pb := a.Platform, b.Platform
		if pa.Architecture != pb.Architecture || pa.OS != pb.OS || pa.OSVersion != pb.OSVersion || pa.Variant != pb.Variant {
			return false
		}
		if !slices.Equal(pa.OSFeatures, pb.OSFeatures) {
			return false
		}
	}
	return true
}

// LoadIndex will load the index from disk.
func (o *OCI) LoadIndex() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.loadIndexLocked()
}

// loadIndexLocked is LoadIndex's implementation. Callers must hold o.mu.
func (o *OCI) loadIndexLocked() error {
	path := o.path(ocispec.ImageIndexFile)
	idx, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		o.index = &ocispec.Index{
			Versioned: specs.Versioned{
				SchemaVersion: 2,
			},
			MediaType: ocispec.MediaTypeImageIndex,
		}
		return nil
	}
	defer idx.Close()

	if err := json.NewDecoder(idx).Decode(&o.index); err != nil {
		return err
	}

	for _, desc := range o.index.Manifests {
		key, err := reference.Parse(desc.Annotations[ocispec.AnnotationRefName])
		if err != nil {
			// skip malformed entries rather than making the entire store unreadable
			continue
		}

		// Set default kind if missing... normalize legacy dev.cosignproject.cosign values
		kind := desc.Annotations[consts.KindAnnotationName]
		kind = consts.NormalizeLegacyKind(kind)
		if kind == "" {
			kind = consts.KindAnnotationImage
		}

		// Write the normalized kind back into a copy of the annotations map so
		// that Walk() callers receive descriptors with dev.hauler/... values.
		// We copy the map to avoid mutating the slice element's shared map.
		normalized := make(map[string]string, len(desc.Annotations)+1)
		maps.Copy(normalized, desc.Annotations)
		normalized[consts.KindAnnotationName] = kind
		desc.Annotations = normalized

		if strings.TrimSpace(key.String()) != "--" {
			switch key.(type) {
			case name.Digest:
				o.nameMap.Store(fmt.Sprintf("%s-%s", key.Context().String(), kind), desc)
			case name.Tag:
				o.nameMap.Store(fmt.Sprintf("%s-%s", key.String(), kind), desc)
			}
		}
	}

	return nil
}

// SaveIndex will update the index on disk.
func (o *OCI) SaveIndex() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.saveIndexLocked()
}

// saveIndexLocked is SaveIndex's implementation. Callers must hold o.mu.
//
// The write to disk is atomic: the marshaled index is written to a uniquely
// named temp file in the same directory as index.json (guaranteeing a
// same-filesystem rename) and then renamed into place. A unique random
// suffix (via os.CreateTemp) rather than a fixed ".tmp" name is required
// here specifically because two concurrently-running hauler processes
// against the same store share no in-process mutex and must never collide
// on the same temp path. This mirrors the pattern already used by
// writeBlobOnce in this file.
func (o *OCI) saveIndexLocked() error {
	var descs []ocispec.Descriptor
	o.nameMap.Range(func(name, desc interface{}) bool {
		n := desc.(ocispec.Descriptor).Annotations[ocispec.AnnotationRefName]
		d := desc.(ocispec.Descriptor)

		if d.Annotations == nil {
			d.Annotations = make(map[string]string)
		}
		d.Annotations[ocispec.AnnotationRefName] = n
		descs = append(descs, d)
		return true
	})

	// sort index to ensure that images come before any signatures and attestations.
	sort.SliceStable(descs, func(i, j int) bool {
		kindI := descs[i].Annotations["kind"]
		kindJ := descs[j].Annotations["kind"]

		// Objects with the prefix of KindAnnotationImage should be at the top.
		if strings.HasPrefix(kindI, consts.KindAnnotationImage) && !strings.HasPrefix(kindJ, consts.KindAnnotationImage) {
			return true
		} else if !strings.HasPrefix(kindI, consts.KindAnnotationImage) && strings.HasPrefix(kindJ, consts.KindAnnotationImage) {
			return false
		}
		return false // Default: maintain the order.
	})

	o.index.Manifests = descs
	data, err := json.Marshal(o.index)
	if err != nil {
		return err
	}

	indexPath := o.path(ocispec.ImageIndexFile)
	dir := filepath.Dir(indexPath)

	tmp, err := os.CreateTemp(dir, "index-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Unconditional cleanup: harmless ENOENT after a successful rename, and
	// it's the cleanup path for every error branch below -- same idiom as
	// writeBlobOnce.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp creates files 0600; index.json must be readable by whatever
	// else reads the store (e.g. `hauler store serve` running as another
	// user), so chmod before rename.
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath)
}

// Resolve attempts to resolve the reference into a name and descriptor.
//
// The argument `ref` should be a scheme-less URI representing the remote.
// Structurally, it has a host and path. The "host" can be used to directly
// reference a specific host or be matched against a specific handler.
//
// The returned name should be used to identify the referenced entity.
// Dependending on the remote namespace, this may be immutable or mutable.
// While the name may differ from ref, it should itself be a valid ref.
//
// If the resolution fails, an error will be returned.
func (o *OCI) Resolve(ctx context.Context, ref string) (ocispec.Descriptor, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.loadIndexLocked(); err != nil {
		return ocispec.Descriptor{}, err
	}
	d, ok := o.nameMap.Load(ref)
	if !ok {
		return ocispec.Descriptor{}, fmt.Errorf("reference %s not found", ref)
	}
	desc := d.(ocispec.Descriptor)
	return desc, nil
}

// Fetcher returns a new fetcher for the provided reference.
// All content fetched from the returned fetcher will be
// from the namespace referred to by ref.
func (o *OCI) Fetcher(ctx context.Context, ref string) (remotes.Fetcher, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.loadIndexLocked(); err != nil {
		return nil, err
	}
	if _, ok := o.nameMap.Load(ref); !ok {
		return nil, nil
	}
	return o, nil
}

// Fetch is intentionally lock-free: it only touches the filesystem (via
// blobReaderAt/ensureBlob) and the immutable root field, never
// index/nameMap. Adding a lock here would deadlock store.Layout.CleanUp,
// which calls Fetch from inside an OCI.Walk callback (see Walk's doc
// comment for the general hazard this would reintroduce).
func (o *OCI) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	readerAt, err := o.blobReaderAt(desc)
	if err != nil {
		return nil, err
	}
	return readerAt, nil
}

// FetchManifest is intentionally lock-free -- see Fetch's doc comment.
func (o *OCI) FetchManifest(ctx context.Context, manifest ocispec.Manifest) (io.ReadCloser, error) {
	readerAt, err := o.manifestBlobReaderAt(manifest)
	if err != nil {
		return nil, err
	}
	return readerAt, nil
}

// Pusher returns a new pusher for the provided reference
// The returned Pusher should satisfy content.Ingester and concurrent attempts
// to push the same blob using the Ingester API should result in ErrUnavailable.
func (o *OCI) Pusher(ctx context.Context, ref string) (remotes.Pusher, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.loadIndexLocked(); err != nil {
		return nil, err
	}

	var baseRef, hash string
	parts := strings.SplitN(ref, "@", 2)
	baseRef = parts[0]

	if len(parts) > 1 {
		hash = parts[1]
	}

	return &ociPusher{
		oci:    o,
		ref:    baseRef,
		digest: hash,
	}, nil
}

// Walk loads the index, takes a full snapshot of nameMap under o.mu -- with
// each descriptor's annotations deep-copied so no caller can mutate a map
// object shared with nameMap -- and only then releases the lock before
// invoking fn once per entry.
//
// This snapshot-then-release shape is load-bearing, not a style choice.
// store.Layout.CopyAll's self-sync path (cmd/hauler/cli/store/sync.go:468
// calls s.CopyAll(ctx, s.OCI, nil)) walks s.OCI and, from inside this Walk's
// callback, calls l.Copy(ctx, reference, to, toRef) with to == l.OCI: the
// very same OCI instance. Copy in turn calls Resolve, Fetcher, and Pusher on
// that instance. store.Layout.CleanUp has the same shape via Fetch. A plain
// sync.Mutex held across the whole Walk call -- including the fn
// invocations -- would deadlock on the very first self-sync, since those
// calls re-enter Lock() on the same OCI from the same goroutine. Releasing
// the lock before calling fn is what makes that re-entrance safe.
//
// The annotation deep-copy matters for a different reason: it means
// in-place mutation of a descriptor's Annotations map inside a Walk
// callback (the pattern the old rewriteReference/chart-rewrite code used)
// silently no-ops on a throwaway copy instead of racing with -- or
// corrupting -- the map object shared with nameMap. Callers that need to
// persist an annotation change must use UpdateAnnotations instead.
func (o *OCI) Walk(fn func(reference string, desc ocispec.Descriptor) error) error {
	o.mu.Lock()
	if err := o.loadIndexLocked(); err != nil {
		o.mu.Unlock()
		return err
	}

	type entry struct {
		key  string
		desc ocispec.Descriptor
	}
	var snapshot []entry
	o.nameMap.Range(func(key, value interface{}) bool {
		d := value.(ocispec.Descriptor)
		cp := make(map[string]string, len(d.Annotations))
		maps.Copy(cp, d.Annotations)
		d.Annotations = cp
		snapshot = append(snapshot, entry{key: key.(string), desc: d})
		return true
	})
	o.mu.Unlock()

	var errst []string
	for _, e := range snapshot {
		if err := fn(e.key, e.desc); err != nil {
			errst = append(errst, err.Error())
		}
	}
	if errst != nil {
		return fmt.Errorf("%s", strings.Join(errst, "; "))
	}
	return nil
}

// blobReaderAt, manifestBlobReaderAt, blobWriterAt, and ensureBlob are all
// intentionally lock-free: they only touch the filesystem (blob paths
// derived from the immutable root field) and never index/nameMap. See
// Fetch's doc comment for why adding a lock here would be a regression, not
// an improvement.
func (o *OCI) blobReaderAt(desc ocispec.Descriptor) (*os.File, error) {
	blobPath, err := o.ensureBlob(desc.Digest.Algorithm().String(), desc.Digest.Hex())
	if err != nil {
		return nil, err
	}
	return os.Open(blobPath)
}

func (o *OCI) manifestBlobReaderAt(manifest ocispec.Manifest) (*os.File, error) {
	blobPath, err := o.ensureBlob(string(manifest.Config.Digest.Algorithm().String()), manifest.Config.Digest.Hex())
	if err != nil {
		return nil, err
	}
	return os.Open(blobPath)
}

func (o *OCI) blobWriterAt(desc ocispec.Descriptor) (*os.File, error) {
	blobPath, err := o.ensureBlob(desc.Digest.Algorithm().String(), desc.Digest.Hex())
	if err != nil {
		return nil, err
	}
	return os.OpenFile(blobPath, os.O_WRONLY|os.O_CREATE, 0644)
}

func (o *OCI) ensureBlob(alg string, hex string) (string, error) {
	dir := o.path(ocispec.ImageBlobsDir, alg)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return "", err
	}
	return filepath.Join(dir, hex), nil
}

// WriteBlob atomically and verifiably writes a blob to the store's blob
// directory, deduplicating concurrent writers of the same digest.
//
// WriteBlob and writeBlobOnce are intentionally lock-free with respect to
// o.mu: they only touch the filesystem (blob paths derived from the
// immutable root field) and the package-private singleflight.Group, never
// index/nameMap. This is unchanged by and unrelated to the index
// thread-safety work elsewhere in this file -- do not add o.mu locking here.
//
// open is a thunk, not a reader: singleflight must not start the download
// until it wins the flight, and a retry (after a digest mismatch or write
// failure) needs a fresh reader rather than a partially-consumed one.
//
// Behavior:
//   - Fast path: if a file already exists at the final blob path and its
//     size matches (when size > 0) or is simply non-empty (when size <= 0),
//     WriteBlob returns immediately without re-hashing or calling open. This
//     keeps re-syncs from becoming O(store size) in disk reads, at the cost
//     of trusting existing content by size alone.
//   - Concurrent callers writing the same digest are deduplicated via a
//     per-OCI singleflight.Group so only one of them actually opens and
//     streams content.
//   - Content is streamed into a temp file in the same directory as the
//     final blob path (guaranteeing same-filesystem atomic rename) while
//     being hashed inline. On success the temp file is chmod'd 0644 (temp
//     files are created 0600, which would make the blob unreadable to e.g.
//     `hauler store serve` running as another user), fsync'd, and renamed
//     into place.
//   - On any error -- including a digest mismatch -- the temp file is
//     removed and the final blob path is left untouched. This is the
//     critical invariant that makes retries and concurrent writers safe:
//     a failing writer can never delete or corrupt a peer's completed blob.
//   - singleflight.Group.Do hands the flight *winner's* error to every
//     waiter, including waiters whose own ctx is a distinct, still-live
//     context. If the shared error is context.Canceled but this caller's own
//     ctx is not done, that cancellation belonged to the winner, not to this
//     caller -- WriteBlob retries once on the caller's own ctx rather than
//     propagating a cancellation the caller never asked for. See
//     writeBlobShared's doc comment for the mechanics.
func (o *OCI) WriteBlob(ctx context.Context, expected digest.Digest, size int64, open func() (io.ReadCloser, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir := o.path(ocispec.ImageBlobsDir, expected.Algorithm().String())
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}
	blobPath := filepath.Join(dir, expected.Hex())

	// Fast path: trust existing content by size alone. Re-hashing every
	// existing blob on every sync would make re-syncs O(store size) in disk
	// reads; the size check alone is enough to catch a truncated/partial
	// blob left behind by a crash. Deliberately checked, and returns,
	// *before* touching blobSem: a cache hit must be free regardless of how
	// saturated the semaphore is.
	if info, err := os.Stat(blobPath); err == nil {
		if size > 0 {
			if info.Size() == size {
				return nil
			}
		} else if info.Size() > 0 {
			return nil
		}
	}

	err := o.writeBlobShared(ctx, dir, blobPath, expected, size, open)
	if err != nil && errors.Is(err, context.Canceled) && ctx.Err() == nil {
		// singleflight.Do hands the flight winner's error to every waiter,
		// including waiters on a distinct, still-live context. If the winner
		// observed *its own* context's cancellation, that's not evidence this
		// caller's request should fail -- retry once on our own ctx. By the
		// time Do() returned, the flight has already been forgotten by
		// singleflight.Group, so this retry either hits the fast path (if the
		// original winner actually finished writing before its cancellation
		// was observed) or this goroutine becomes the new flight leader.
		err = o.writeBlobShared(ctx, dir, blobPath, expected, size, open)
	}
	return err
}

// writeBlobShared runs open()/writeBlobOnce for expected under this OCI's
// singleflight.Group, deduplicating concurrent writers of the same digest so
// only one of them actually streams content.
func (o *OCI) writeBlobShared(ctx context.Context, dir, blobPath string, expected digest.Digest, size int64, open func() (io.ReadCloser, error)) error {
	_, err, _ := o.sf.Do(expected.String(), func() (interface{}, error) {
		// Acquired inside the singleflight function, not around the sf.Do
		// call: goroutines that lose the flight and are merely waiting on
		// Do() to return must not hold a permit for the duration of someone
		// else's write -- only the one goroutine actually about to stream
		// bytes should count against the bound.
		if err := o.blobSem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer o.blobSem.Release(1)
		return nil, o.writeBlobOnce(ctx, dir, blobPath, expected, size, open)
	})
	return err
}

// writeBlobOnce performs the actual temp-file-then-rename write for a single
// digest. It is only ever invoked by the singleflight winner in WriteBlob,
// which already holds a blobSem permit for the duration of this call.
func (o *OCI) writeBlobOnce(ctx context.Context, dir, blobPath string, expected digest.Digest, size int64, open func() (io.ReadCloser, error)) (err error) {
	// Re-check under the flight: another WriteBlob call (from a different
	// singleflight key generation, i.e. a prior flight that already
	// completed) may have written this blob while we were waiting to start.
	if info, statErr := os.Stat(blobPath); statErr == nil {
		if size > 0 && info.Size() == size {
			return nil
		}
		if size <= 0 && info.Size() > 0 {
			return nil
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, expected.Hex()+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// Unconditional cleanup: harmless ENOENT after a successful rename, and
	// it's the cleanup path for every error branch below.
	defer os.Remove(tmpPath)

	rc, err := open()
	if err != nil {
		tmp.Close()
		return err
	}

	// Wrap the reader rather than plumbing ctx into open()'s underlying
	// implementation (v1.Layer.Compressed() and friends, which this package
	// doesn't control): once ctx is done, Read starts returning ctx.Err()
	// instead of delegating, so io.Copy below aborts between chunks rather
	// than running an already-cancelled download to completion.
	cr := &ctxReader{ctx: ctx, r: rc}

	dg := digest.Canonical.Digester()
	n, copyErr := io.Copy(io.MultiWriter(tmp, dg.Hash()), cr)
	closeReadErr := rc.Close()

	if copyErr != nil {
		tmp.Close()
		return copyErr
	}
	if closeReadErr != nil {
		tmp.Close()
		return closeReadErr
	}
	if size > 0 && n != size {
		tmp.Close()
		return fmt.Errorf("content: short/long write for %s: wrote %d bytes, expected %d: %w", expected, n, size, ErrDigestMismatch)
	}

	got := dg.Digest()
	if got != expected {
		tmp.Close()
		return fmt.Errorf("content: digest mismatch for blob: expected %s, got %s (%d bytes): %w", expected, got, n, ErrDigestMismatch)
	}

	// Commit: chmod before rename (temp files are created 0600), fsync
	// before close, then atomically rename into place.
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// os.Rename silently replaces an existing target, which is correct here:
	// the content is digest-identical to whatever else could already be at
	// blobPath.
	return os.Rename(tmpPath, blobPath)
}

// path and IndexExists are intentionally lock-free -- see Fetch's doc
// comment. They only touch the immutable root field and, for IndexExists,
// the filesystem.
func (o *OCI) path(elem ...string) string {
	complete := []string{string(o.root)}
	return filepath.Join(append(complete, elem...)...)
}

// IndexExists reports whether the store's OCI layout index.json exists on disk.
func (o *OCI) IndexExists() bool {
	_, err := os.Stat(o.path(ocispec.ImageIndexFile))
	return err == nil
}

type ociPusher struct {
	oci    *OCI
	ref    string
	digest string
}

// Push returns a content writer for the given resource identified
// by the descriptor.
func (p *ociPusher) Push(ctx context.Context, d ocispec.Descriptor) (ccontent.Writer, error) {
	switch d.MediaType {
	case ocispec.MediaTypeImageManifest, ocispec.MediaTypeImageIndex, consts.DockerManifestSchema2, consts.DockerManifestListSchema2:
		// if the hash of the content matches that which was provided as the hash for the root, mark it
		if p.digest != "" && p.digest == d.Digest.String() {
			// This load-modify-save is one logical read-modify-write unit and
			// must be a single critical section, not three separate lock
			// acquire/release cycles -- otherwise another goroutine's index
			// save could land between this Push's load and its save, silently
			// discarding it. Call the Locked variants directly: calling the
			// exported LoadIndex/SaveIndex here would deadlock on this same
			// lock.
			p.oci.mu.Lock()
			if err := p.oci.loadIndexLocked(); err != nil {
				p.oci.mu.Unlock()
				return nil, err
			}
			// Use compound key format: "reference-kind"; normalize legacy values.
			kind := d.Annotations[consts.KindAnnotationName]
			kind = consts.NormalizeLegacyKind(kind)
			if kind == "" {
				kind = consts.KindAnnotationImage
			}
			// Copy annotations map to avoid mutating the caller's descriptor,
			// then write the normalized kind so Walk() callers see dev.hauler/... values.
			normalizedAnnotations := make(map[string]string, len(d.Annotations)+1)
			maps.Copy(normalizedAnnotations, d.Annotations)
			normalizedAnnotations[consts.KindAnnotationName] = kind
			d.Annotations = normalizedAnnotations
			key := fmt.Sprintf("%s-%s", p.ref, kind)
			p.oci.nameMap.Store(key, d)
			err := p.oci.saveIndexLocked()
			p.oci.mu.Unlock()
			if err != nil {
				return nil, err
			}
		}
	}

	blobPath, err := p.oci.ensureBlob(d.Digest.Algorithm().String(), d.Digest.Hex())
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(blobPath); err == nil {
		// file already exists, discard (but validate digest). Deliberately
		// returned before touching blobSem -- same reasoning as WriteBlob's
		// fast path: a cache hit must be free regardless of how saturated the
		// semaphore is.
		return NewIoContentWriter(nopCloser{io.Discard}, WithOutputHash(d.Digest.String())), nil
	}

	// Share the same bound as WriteBlob: a permit is held for the writer's
	// entire lifetime (Push through Close), since unlike WriteBlob -- which
	// owns the whole read-and-write loop itself -- the caller streams bytes
	// into the returned writer over an arbitrary number of separate Write
	// calls. Acquire is ctx-aware, so a caller that gives up while queued
	// behind the bound gets ctx.Err() back instead of blocking forever.
	if err := p.oci.blobSem.Acquire(ctx, 1); err != nil {
		return nil, err
	}

	w, err := newOCIBlobWriter(filepath.Dir(blobPath), blobPath, d.Digest.String())
	if err != nil {
		p.oci.blobSem.Release(1)
		return nil, err
	}
	w.releaseSem = func() { p.oci.blobSem.Release(1) }
	return w, nil
}

// ociBlobWriter streams pushed content into a temp file in the same
// directory as the final blob path and, on successful digest verification at
// Close, atomically renames it into place. On any error -- write failure,
// digest mismatch, or a close/sync failure -- the temp file is discarded and
// the final blob path is left untouched, matching the invariant upheld by
// content.OCI.WriteBlob: a failed writer can never delete or corrupt content
// a peer already committed.
type ociBlobWriter struct {
	tmp        *os.File
	tmpPath    string
	finalPath  string
	digester   digest.Digester
	status     ccontent.Status
	outputHash string

	// releaseSem releases the blobSem permit acquired by ociPusher.Push for
	// this writer's lifetime. nil when Push served this writer from a path
	// that never acquired one (there currently is none, but nil-checking
	// keeps this type constructible in tests without going through Push).
	releaseSem func()
}

var _ ccontent.Writer = (*ociBlobWriter)(nil)

func newOCIBlobWriter(dir, finalPath, outputHash string) (*ociBlobWriter, error) {
	tmp, err := os.CreateTemp(dir, filepath.Base(finalPath)+".tmp-*")
	if err != nil {
		return nil, err
	}
	return &ociBlobWriter{
		tmp:        tmp,
		tmpPath:    tmp.Name(),
		finalPath:  finalPath,
		digester:   digest.Canonical.Digester(),
		outputHash: outputHash,
	}, nil
}

// Write writes data to the temp file and updates the running digest.
func (w *ociBlobWriter) Write(p []byte) (int, error) {
	n, err := w.tmp.Write(p)
	if n > 0 {
		w.digester.Hash().Write(p[:n])
	}
	return n, err
}

// Close verifies the digest and, only on success, chmods, syncs, closes, and
// atomically renames the temp file into the final blob path. On any failure
// the temp file is closed (if not already) and left for the deferred
// os.Remove; the final blob path is never touched.
func (w *ociBlobWriter) Close() (err error) {
	if w.releaseSem != nil {
		defer w.releaseSem() // released on every path below, success or failure
	}
	defer os.Remove(w.tmpPath) // unconditional: harmless ENOENT after a successful rename

	if w.outputHash != "" {
		if computed := w.digester.Digest().String(); computed != w.outputHash {
			w.tmp.Close()
			return fmt.Errorf("digest mismatch: expected %s, got %s", w.outputHash, computed)
		}
	}

	if err := w.tmp.Chmod(0644); err != nil {
		w.tmp.Close()
		return err
	}
	if err := w.tmp.Sync(); err != nil {
		w.tmp.Close()
		return err
	}
	if err := w.tmp.Close(); err != nil {
		return err
	}

	// os.Rename silently replaces an existing target, which is correct here:
	// the content is digest-verified to match what's expected at finalPath.
	return os.Rename(w.tmpPath, w.finalPath)
}

// Digest returns the current digest of written data.
func (w *ociBlobWriter) Digest() digest.Digest {
	return w.digester.Digest()
}

// Commit is a no-op for this implementation, matching IoContentWriter.
func (w *ociBlobWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...ccontent.Opt) error {
	return nil
}

// Status returns the current status.
func (w *ociBlobWriter) Status() (ccontent.Status, error) {
	return w.status, nil
}

// Truncate is not supported.
func (w *ociBlobWriter) Truncate(size int64) error {
	return fmt.Errorf("truncate not supported")
}

// RemoveFromIndex removes ref from nameMap only; it does not save the index
// to disk. Matches the pre-existing behavior: callers (e.g.
// store.Layout.RemoveArtifact) call SaveIndex separately afterward.
func (o *OCI) RemoveFromIndex(ref string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nameMap.Delete(ref)
}

// UpdateAnnotations locates every descriptor for which match returns true and
// replaces its annotations with a fresh copy that has had apply run over it,
// then persists the index. It returns the number of descriptors matched, so
// callers can distinguish "found and updated" from "not found" the same way
// they did when mutating Walk callback descriptors in place -- before that
// pattern became unsafe under concurrent access (see Walk's doc comment).
//
// The whole match/apply/store pass runs as a single critical section so a
// concurrent UpdateAnnotations or Push can't interleave a save between this
// call's load and its save. When nothing matches, the index is not
// re-saved: a no-op UpdateAnnotations call must not touch index.json.
func (o *OCI) UpdateAnnotations(match func(ocispec.Descriptor) bool, apply func(map[string]string)) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.loadIndexLocked(); err != nil {
		return 0, err
	}

	matched := 0
	o.nameMap.Range(func(key, value interface{}) bool {
		d := value.(ocispec.Descriptor)
		if !match(d) {
			return true
		}
		cp := make(map[string]string, len(d.Annotations))
		maps.Copy(cp, d.Annotations)
		apply(cp)
		d.Annotations = cp
		o.nameMap.Store(key, d)
		matched++
		return true
	})

	if matched == 0 {
		return 0, nil
	}
	return matched, o.saveIndexLocked()
}

// ResolvePath returns the absolute path for a given relative path within the OCI root
func (o *OCI) ResolvePath(elem string) string {
	if elem == "" {
		return o.root
	}
	return filepath.Join(o.root, elem)
}

// nopCloser wraps an io.Writer to implement io.WriteCloser
type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }
