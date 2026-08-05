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
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

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

// indexCheckpointInterval bounds fsync frequency on the per-artifact save
// path: index.json is rewritten in full on every AddIndex, so fsyncing each
// one costs O(N^2) bytes across a sync (~5 GB for 5,000 artifacts) while
// holding o.mu. Coalescing trades a bounded power-loss/panic exposure window
// for that cost -- an interrupted sync is simply re-run.
const indexCheckpointInterval = 30 * time.Second

// ErrDigestMismatch is returned by WriteBlob (and wrapped with details) when
// the content actually streamed from open() does not hash to the expected
// digest. Callers can retry: the final blob path is never touched on this
// error, so a fresh WriteBlob call will re-download cleanly.
var ErrDigestMismatch = errors.New("content: digest mismatch")

// ctxReader wraps an io.Reader so Read returns ctx.Err() once ctx is done,
// instead of delegating. This makes an in-flight WriteBlob copy abort on
// cancellation, since the underlying reader (v1.Layer's Compressed(), which
// this package doesn't control) has no cancellation hook of its own.
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

	// blobSem bounds blob writes in flight across this OCI's two write
	// paths (WriteBlob, ociPusher.Push), scoped per-store. Acquire is
	// ctx-aware, unlike errgroup.SetLimit, so it returns promptly on
	// cancellation instead of leaving a goroutine parked.
	blobSem *semaphore.Weighted

	// blobConcurrency is the ceiling blobSem was built with, retained so
	// reporting can render "peak-inflight=N/ceiling" (semaphore.Weighted
	// doesn't expose its own capacity).
	blobConcurrency int

	// stats accumulates disk-contention counters for this store. See
	// stats.go.
	stats IOStats

	// mu guards index, index.json on disk, and nameMap descriptors'
	// annotation maps. Exported methods are thin lock-then-Locked-variant
	// wrappers; the *Locked methods assume the caller holds mu, letting
	// internal chains (e.g. ociPusher.Push's load-modify-save) re-enter
	// without double-locking (see Walk's doc comment for the related hazard).
	mu sync.Mutex

	// lastDurableSave is when the index was last fsync'd, guarded by mu.
	// The zero value makes the first save of a run durable, which gives an
	// early checkpoint for free.
	lastDurableSave time.Time

	// now is time.Now, replaced in tests so checkpoint-interval behavior
	// can be exercised without sleeping.
	now func() time.Time
}

// lock acquires o.mu, recording time spent blocked into IOStats, so every
// caller measures index-serialization cost in one place. Unlock isn't
// wrapped since it never blocks.
func (o *OCI) lock() {
	start := time.Now()
	o.mu.Lock()
	o.stats.IndexLockWaitNanos.Add(int64(time.Since(start)))
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
			o.blobConcurrency = n
		}
	}
}

func NewOCI(root string, opts ...OCIOption) (*OCI, error) {
	o := &OCI{
		root:            root,
		nameMap:         &sync.Map{},
		blobSem:         semaphore.NewWeighted(consts.DefaultBlobConcurrency),
		blobConcurrency: consts.DefaultBlobConcurrency,
		now:             time.Now,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o, nil
}

// Stats returns this store's I/O contention counters. The returned pointer
// is live -- counters keep incrementing as work proceeds. Call Snapshot on
// it to take a stable reading.
func (o *OCI) Stats() *IOStats {
	return &o.stats
}

// BlobConcurrency returns the ceiling blobSem was constructed with.
func (o *OCI) BlobConcurrency() int {
	return o.blobConcurrency
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

	o.lock()
	defer o.mu.Unlock()

	// Skip the write when the stored descriptor is already byte-identical:
	// index.json rewrites aren't otherwise batched (O(N^2) bytes as the
	// index grows), only their fsync is (see indexCheckpointInterval).
	if existing, ok := o.nameMap.Load(mapKey); ok {
		if descriptorsEqual(existing.(ocispec.Descriptor), desc) {
			return nil
		}
	}

	o.nameMap.Store(mapKey, desc)
	return o.saveIndexCheckpointLocked()
}

// descriptorsEqual reports whether two descriptors are equal in every field
// AddIndex's callers in this codebase populate: MediaType, Digest, Size,
// URLs, ArtifactType, Platform, Data, and Annotations (compared by
// contents, not map identity).
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
	o.lock()
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

		// Write normalized kind into a copy of Annotations so Walk() callers
		// see it, without mutating the slice element's shared map.
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
	o.lock()
	defer o.mu.Unlock()
	return o.saveIndexLocked(true)
}

// saveIndexLocked is SaveIndex's implementation. Callers must hold o.mu.
//
// The write is atomic: temp file (uniquely named via os.CreateTemp, since
// two hauler processes share no in-process mutex) in the same directory,
// then renamed into place.
//
// durable controls both the temp file's fsync and, after rename, an fsync of
// the containing directory (see syncDir); when false the write is still
// atomic but may not survive power loss until a later save catches up --
// see indexCheckpointInterval.
func (o *OCI) saveIndexLocked(durable bool) error {
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
	// chmod before rename: CreateTemp files are 0600, but index.json must be
	// readable by other consumers (e.g. `hauler store serve` as another user).
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if durable {
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		return err
	}
	// Track the rename unconditionally -- it succeeded regardless of whether
	// the durable branch below (which can still fail on syncDir) completes.
	// IndexDurableWrites stays inside that branch since it must count only
	// fsyncs that actually completed.
	o.stats.IndexWrites.Add(1)
	o.stats.IndexBytesWritten.Add(int64(len(data)))
	if durable {
		if err := syncDir(dir); err != nil {
			return err
		}
		o.lastDurableSave = o.now()
		o.stats.IndexDurableWrites.Add(1)
	}
	return nil
}

// saveIndexCheckpointLocked saves the index, fsync'ing only when at least
// indexCheckpointInterval has elapsed since the last durable save. Used by
// the per-artifact callers (AddIndex, ociPusher.Push); callers that are
// explicit checkpoints call saveIndexLocked(true) directly. Callers must
// hold o.mu.
func (o *OCI) saveIndexCheckpointLocked() error {
	return o.saveIndexLocked(o.now().Sub(o.lastDurableSave) >= indexCheckpointInterval)
}

// syncDir fsyncs a directory so a rename into it survives power loss;
// fsyncing the file alone doesn't guarantee the directory entry pointing at
// it (containerd's local content store does the same after a blob rename).
// A no-op on Windows, which has no equivalent. EINVAL/ENOTSUP are tolerated
// (some NFS/SMB mounts, common for an air-gapped store root, don't support
// directory fsync) rather than turning a previously working command into a
// hard failure; every other error is still fatal.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
			return nil
		}
		return err
	}
	return d.Close()
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
	o.lock()
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
	o.lock()
	defer o.mu.Unlock()

	if err := o.loadIndexLocked(); err != nil {
		return nil, err
	}
	if _, ok := o.nameMap.Load(ref); !ok {
		return nil, nil
	}
	return o, nil
}

// Fetch is intentionally lock-free: it only touches the filesystem and the
// immutable root field, never index/nameMap. A lock here would deadlock
// store.Layout.CleanUp, which calls Fetch from inside an OCI.Walk callback
// (see Walk's doc comment for the general hazard).
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
	o.lock()
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

// Walk loads the index, snapshots nameMap under o.mu -- deep-copying each
// descriptor's annotations -- then releases the lock before invoking fn per
// entry. This is load-bearing: store.Layout.CopyAll's self-sync path
// re-enters Copy/Resolve/Fetcher/Pusher on this same OCI from inside a Walk
// callback (CleanUp does the same via Fetch), and a plain mutex held across
// fn would deadlock on that re-entrant Lock() from the same goroutine.
//
// The deep copy means in-place mutation of a callback's descriptor silently
// no-ops instead of corrupting nameMap's shared map; use UpdateAnnotations
// to persist changes.
func (o *OCI) Walk(fn func(reference string, desc ocispec.Descriptor) error) error {
	o.lock()
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

// blobReaderAt, manifestBlobReaderAt, blobWriterAt, and ensureBlob are
// lock-free too -- see Fetch's doc comment.
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
// directory, deduplicating concurrent writers of the same digest. It is
// lock-free with respect to o.mu -- see Fetch's doc comment. open is a
// thunk, not a reader, since singleflight must not start the download until
// it wins the flight, and a retry needs a fresh reader.
//
// A file already at the final path with a matching size short-circuits
// WriteBlob without re-hashing, so re-syncs don't become O(store size) in
// disk reads. Otherwise content streams, deduplicated via singleflight, into
// a temp file hashed inline, then chmod'd, fsync'd, and renamed into place.
// On any error the temp file is removed and the final path untouched, so a
// failing writer can't corrupt a peer's completed blob and a retry
// re-downloads cleanly.
//
// singleflight.Do hands the flight winner's error to every waiter, even ones
// on a distinct, still-live ctx -- if that error is context.Canceled but
// this caller's ctx isn't done, WriteBlob retries once on the caller's own
// ctx rather than propagate a cancellation that wasn't its own.
func (o *OCI) WriteBlob(ctx context.Context, expected digest.Digest, size int64, open func() (io.ReadCloser, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir := o.path(ocispec.ImageBlobsDir, expected.Algorithm().String())
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}
	blobPath := filepath.Join(dir, expected.Hex())

	// Fast path: trust existing content by size alone, checked and returned
	// before touching blobSem so a cache hit stays free regardless of
	// semaphore saturation.
	if info, err := os.Stat(blobPath); err == nil {
		if size > 0 {
			if info.Size() == size {
				o.stats.BlobsCached.Add(1)
				return nil
			}
		} else if info.Size() > 0 {
			o.stats.BlobsCached.Add(1)
			return nil
		}
	}

	err := o.writeBlobShared(ctx, dir, blobPath, expected, size, open)
	if err != nil && errors.Is(err, context.Canceled) && ctx.Err() == nil {
		// See WriteBlob's doc comment: retrying either hits the fast path or
		// this goroutine becomes the new flight leader.
		err = o.writeBlobShared(ctx, dir, blobPath, expected, size, open)
	}
	return err
}

// writeBlobShared dedupes concurrent writers of expected via this OCI's
// singleflight.Group so only one actually streams content.
func (o *OCI) writeBlobShared(ctx context.Context, dir, blobPath string, expected digest.Digest, size int64, open func() (io.ReadCloser, error)) error {
	_, err, _ := o.sf.Do(expected.String(), func() (interface{}, error) {
		// Acquired inside the singleflight func, not around sf.Do: losers
		// merely waiting on Do() must not hold a permit for someone else's
		// write.
		start := time.Now()
		if err := o.blobSem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		o.stats.addSemWait(time.Since(start))
		o.stats.enterBlob()
		defer func() {
			o.stats.exitBlob()
			o.blobSem.Release(1)
		}()
		return nil, o.writeBlobOnce(ctx, dir, blobPath, expected, size, open)
	})
	return err
}

// writeBlobOnce performs the temp-file-then-rename write. Only ever invoked
// by the singleflight winner, which already holds a blobSem permit.
func (o *OCI) writeBlobOnce(ctx context.Context, dir, blobPath string, expected digest.Digest, size int64, open func() (io.ReadCloser, error)) (err error) {
	// Re-check under the flight: a prior, already-completed flight may have
	// written this blob while we were waiting to start.
	if info, statErr := os.Stat(blobPath); statErr == nil {
		if size > 0 && info.Size() == size {
			o.stats.BlobsCached.Add(1)
			return nil
		}
		if size <= 0 && info.Size() > 0 {
			o.stats.BlobsCached.Add(1)
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

	// See ctxReader's doc comment: this makes io.Copy below abort between
	// chunks instead of running an already-cancelled download to completion.
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

	// Commit: chmod 0600->0644 (see saveIndexLocked), fsync, then rename.
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
	// os.Rename silently replaces an existing target, which is correct: the
	// content is digest-identical to whatever's already at blobPath.
	if err := os.Rename(tmpPath, blobPath); err != nil {
		return err
	}
	o.stats.BlobsWritten.Add(1)
	o.stats.BlobBytesWritten.Add(n)
	return nil
}

// path and IndexExists are lock-free too -- see Fetch's doc comment.
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
			// Single critical section (Locked variants, to avoid deadlocking
			// on this same lock) so no other save can land in between.
			p.oci.lock()
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
			err := p.oci.saveIndexCheckpointLocked()
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
		// Already exists: discard but validate digest. Returned before
		// touching blobSem -- same reasoning as WriteBlob's fast path.
		return NewIoContentWriter(nopCloser{io.Discard}, WithOutputHash(d.Digest.String())), nil
	}

	// Shares WriteBlob's bound, but held for the writer's whole lifetime
	// (Push through Close) since the caller streams via separate Write
	// calls rather than one owned loop.
	start := time.Now()
	if err := p.oci.blobSem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	p.oci.stats.addSemWait(time.Since(start))
	p.oci.stats.enterBlob()

	w, err := newOCIBlobWriter(filepath.Dir(blobPath), blobPath, d.Digest.String())
	if err != nil {
		p.oci.stats.exitBlob()
		p.oci.blobSem.Release(1)
		return nil, err
	}
	w.releaseSem = func() {
		p.oci.stats.exitBlob()
		p.oci.blobSem.Release(1)
	}
	return w, nil
}

// ociBlobWriter streams pushed content into a temp file and, on successful
// digest verification at Close, renames it into place -- same on-error
// invariant as content.OCI.WriteBlob (see its doc comment).
type ociBlobWriter struct {
	tmp        *os.File
	tmpPath    string
	finalPath  string
	digester   digest.Digester
	status     ccontent.Status
	outputHash string

	// releaseSem releases the blobSem permit acquired by ociPusher.Push. nil
	// when constructed outside Push (e.g. in tests).
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

func (w *ociBlobWriter) Write(p []byte) (int, error) {
	n, err := w.tmp.Write(p)
	if n > 0 {
		w.digester.Hash().Write(p[:n])
	}
	return n, err
}

// Close verifies the digest and, only on success, chmods, syncs, closes, and
// renames the temp file into place; on failure the temp file is left for
// the deferred os.Remove and the final path is untouched.
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

func (w *ociBlobWriter) Digest() digest.Digest {
	return w.digester.Digest()
}

func (w *ociBlobWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...ccontent.Opt) error {
	return nil
}

func (w *ociBlobWriter) Status() (ccontent.Status, error) {
	return w.status, nil
}

func (w *ociBlobWriter) Truncate(size int64) error {
	return fmt.Errorf("truncate not supported")
}

// RemoveFromIndex removes ref from nameMap only; callers (e.g.
// store.Layout.RemoveArtifact) call SaveIndex separately afterward.
func (o *OCI) RemoveFromIndex(ref string) {
	o.lock()
	defer o.mu.Unlock()
	o.nameMap.Delete(ref)
}

// UpdateAnnotations locates every descriptor for which match returns true,
// replaces its annotations with a copy that has had apply run over it, and
// persists the index, returning the number matched. The whole pass runs as
// a single critical section so a concurrent UpdateAnnotations or Push can't
// interleave a save in between; when nothing matches, index.json is not
// re-saved.
func (o *OCI) UpdateAnnotations(match func(ocispec.Descriptor) bool, apply func(map[string]string)) (int, error) {
	o.lock()
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
	return matched, o.saveIndexLocked(true)
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
