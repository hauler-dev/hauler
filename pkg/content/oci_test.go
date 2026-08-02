package content

// oci_test.go covers the annotation-normalization correctness of LoadIndex()
// and ociPusher.Push(). Specifically, it verifies that descriptors returned
// by Walk() carry the normalized dev.hauler/... kind annotation value, not the
// legacy dev.cosignproject.cosign/... value that may be present on disk.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// buildMinimalOCILayout writes the smallest valid OCI layout (oci-layout marker
// + index.json with the supplied descriptors) into dir. No blobs are written;
// this is sufficient for testing LoadIndex/Walk without a full store.
func buildMinimalOCILayout(t *testing.T, dir string, manifests []ocispec.Descriptor) {
	t.Helper()

	// oci-layout marker
	layoutMarker := map[string]string{"imageLayoutVersion": "1.0.0"}
	markerData, err := json.Marshal(layoutMarker)
	if err != nil {
		t.Fatalf("marshal oci-layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ocispec.ImageLayoutFile), markerData, 0644); err != nil {
		t.Fatalf("write oci-layout: %v", err)
	}

	// index.json
	idx := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal index.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), data, 0644); err != nil {
		t.Fatalf("write index.json: %v", err)
	}
}

// fakeDigest returns a syntactically valid digest string that can be used in
// test descriptors without any real blob.
func fakeDigest(hex string) digest.Digest {
	// pad hex to 64 chars
	for len(hex) < 64 {
		hex += "0"
	}
	return digest.Digest("sha256:" + hex)
}

// --------------------------------------------------------------------------
// TestLoadIndex_NormalizesLegacyKindInDescriptorAnnotations
// --------------------------------------------------------------------------

// TestLoadIndex_NormalizesLegacyKindInDescriptorAnnotations verifies that
// after LoadIndex() (called implicitly by Walk()), every descriptor returned
// by Walk carries a normalized dev.hauler/... kind annotation, not the legacy
// dev.cosignproject.cosign/... value stored on disk.
func TestLoadIndex_NormalizesLegacyKindInDescriptorAnnotations(t *testing.T) {
	dir := t.TempDir()

	legacyKinds := []string{
		"dev.cosignproject.cosign/image",
		"dev.cosignproject.cosign/imageIndex",
		"dev.cosignproject.cosign/sigs",
		"dev.cosignproject.cosign/atts",
		"dev.cosignproject.cosign/sboms",
	}

	var manifests []ocispec.Descriptor
	for i, legacyKind := range legacyKinds {
		d := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    fakeDigest(strings.Repeat(string(rune('a'+i)), 1)),
			Size:      100,
			Annotations: map[string]string{
				ocispec.AnnotationRefName: "example.com/repo:tag" + strings.Repeat(string(rune('a'+i)), 1),
				consts.KindAnnotationName: legacyKind,
			},
		}
		manifests = append(manifests, d)
	}

	buildMinimalOCILayout(t, dir, manifests)

	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	var walked []ocispec.Descriptor
	if err := o.Walk(func(_ string, desc ocispec.Descriptor) error {
		walked = append(walked, desc)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(walked) == 0 {
		t.Fatal("Walk returned no descriptors")
	}

	const legacyPrefix = "dev.cosignproject.cosign"
	const newPrefix = "dev.hauler"
	for _, desc := range walked {
		kind := desc.Annotations[consts.KindAnnotationName]
		if strings.HasPrefix(kind, legacyPrefix) {
			t.Errorf("descriptor %s: Walk returned legacy kind %q... want normalized dev.hauler/... value",
				desc.Digest, kind)
		}
		if !strings.HasPrefix(kind, newPrefix) {
			t.Errorf("descriptor %s: Walk returned unexpected kind %q... want dev.hauler/... prefix",
				desc.Digest, kind)
		}
	}
}

// --------------------------------------------------------------------------
// TestLoadIndex_DoesNotMutateOnDiskAnnotations
// --------------------------------------------------------------------------

// TestLoadIndex_DoesNotMutateOnDiskAnnotations verifies that the normalization
// performed by LoadIndex() is in-memory only: the index.json on disk must
// still carry the original (legacy) annotation values after a Walk() call.
func TestLoadIndex_DoesNotMutateOnDiskAnnotations(t *testing.T) {
	dir := t.TempDir()

	legacyKind := "dev.cosignproject.cosign/image"
	manifests := []ocispec.Descriptor{
		{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    fakeDigest("b"),
			Size:      100,
			Annotations: map[string]string{
				ocispec.AnnotationRefName: "example.com/repo:tagb",
				consts.KindAnnotationName: legacyKind,
			},
		},
	}
	buildMinimalOCILayout(t, dir, manifests)

	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	// Trigger LoadIndex via Walk.
	if err := o.Walk(func(_ string, _ ocispec.Descriptor) error { return nil }); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// Re-read index.json from disk and verify the annotation is unchanged.
	data, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index.json: %v", err)
	}
	for _, desc := range idx.Manifests {
		got := desc.Annotations[consts.KindAnnotationName]
		if got != legacyKind {
			t.Errorf("on-disk kind was mutated: got %q, want %q", got, legacyKind)
		}
	}
}

// --------------------------------------------------------------------------
// TestPush_NormalizesLegacyKindInStoredDescriptor
// --------------------------------------------------------------------------

// TestPush_NormalizesLegacyKindInStoredDescriptor verifies that after a Push()
// that matches the root digest, the descriptor stored in nameMap (and therefore
// returned by subsequent Walk() calls) carries the normalized dev.hauler/...
// kind annotation rather than the legacy value.
func TestPush_NormalizesLegacyKindInStoredDescriptor(t *testing.T) {
	dir := t.TempDir()
	buildMinimalOCILayout(t, dir, nil) // start with empty index

	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	// Build a minimal manifest blob so Push() can write it to disk.
	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    fakeDigest("config0"),
			Size:      2,
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestDigest := digest.FromBytes(manifestData)

	// Ensure the blobs directory exists so Push can write.
	blobsDir := filepath.Join(dir, ocispec.ImageBlobsDir, "sha256")
	if err := os.MkdirAll(blobsDir, 0755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	legacyKind := "dev.cosignproject.cosign/sigs"
	baseRef := "example.com/repo:tagsig"

	pusher, err := o.Pusher(context.Background(), baseRef+"@"+manifestDigest.String())
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestData)),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: baseRef,
			consts.KindAnnotationName: legacyKind,
		},
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := w.Write(manifestData); err != nil {
		t.Fatalf("Write manifest: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	// Now Walk and verify the descriptor in nameMap has the normalized kind.
	// We need a fresh OCI instance so Walk calls LoadIndex (which reads SaveIndex output).
	o2, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI second: %v", err)
	}

	const legacyPrefix = "dev.cosignproject.cosign"
	const newPrefix = "dev.hauler"
	var found bool
	if err := o2.Walk(func(_ string, d ocispec.Descriptor) error {
		found = true
		kind := d.Annotations[consts.KindAnnotationName]
		if strings.HasPrefix(kind, legacyPrefix) {
			t.Errorf("Push stored descriptor with legacy kind %q... want normalized dev.hauler/... value", kind)
		}
		if !strings.HasPrefix(kind, newPrefix) {
			t.Errorf("Push stored descriptor with unexpected kind %q... want dev.hauler/... prefix", kind)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !found {
		t.Fatal("Walk returned no descriptors after Push")
	}

	// Also verify the caller's original descriptor map was NOT mutated.
	if desc.Annotations[consts.KindAnnotationName] != legacyKind {
		t.Errorf("Push mutated caller's descriptor annotations: got %q, want %q",
			desc.Annotations[consts.KindAnnotationName], legacyKind)
	}
}

// blobPathFor mirrors the layout convention used by OCI.ensureBlob, for
// assertions against the final on-disk blob path.
func blobPathFor(root string, d digest.Digest) string {
	return filepath.Join(root, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
}

// countTmpFiles returns the number of *.tmp-* files left behind in the
// sha256 blobs directory under root.
func countTmpFiles(t *testing.T, root string) int {
	t.Helper()
	dir := filepath.Join(root, ocispec.ImageBlobsDir, "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	count := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			count++
		}
	}
	return count
}

func TestWriteBlob_WritesAndVerifiesNewBlob(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := []byte("hello world, this is blob content")
	d := digest.FromBytes(data)

	err = o.WriteBlob(context.Background(), d, int64(len(data)), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	})
	if err != nil {
		t.Fatalf("WriteBlob: unexpected error: %v", err)
	}

	blobPath := blobPathFor(dir, d)
	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading written blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}

	if info, err := os.Stat(blobPath); err != nil {
		t.Fatalf("stat blob: %v", err)
	} else if info.Mode().Perm() != 0644 {
		t.Errorf("blob mode = %o, want 0644", info.Mode().Perm())
	}

	if n := countTmpFiles(t, dir); n != 0 {
		t.Errorf("left %d temp files behind, want 0", n)
	}
}

func TestWriteBlob_FastPath_SkipsOpenWhenExistingSizeMatches(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := []byte("existing correct content")
	d := digest.FromBytes(data)
	blobPath := blobPathFor(dir, d)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(blobPath, data, 0644); err != nil {
		t.Fatalf("pre-writing blob: %v", err)
	}

	var openCalled int32
	err = o.WriteBlob(context.Background(), d, int64(len(data)), func() (io.ReadCloser, error) {
		atomic.AddInt32(&openCalled, 1)
		return nil, errors.New("open should not have been called")
	})
	if err != nil {
		t.Fatalf("WriteBlob: unexpected error: %v", err)
	}
	if openCalled != 0 {
		t.Errorf("open() was called %d times, want 0 (fast path should have skipped it)", openCalled)
	}
}

func TestWriteBlob_FastPath_SizeMismatchTriggersRewrite(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	correct := []byte("this is the correct, full-length blob content")
	d := digest.FromBytes(correct)
	blobPath := blobPathFor(dir, d)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Simulate a truncated/corrupt blob left behind by a crash: short content
	// at the correct path.
	if err := os.WriteFile(blobPath, []byte("short"), 0644); err != nil {
		t.Fatalf("pre-writing truncated blob: %v", err)
	}

	var openCalled int32
	err = o.WriteBlob(context.Background(), d, int64(len(correct)), func() (io.ReadCloser, error) {
		atomic.AddInt32(&openCalled, 1)
		return io.NopCloser(bytes.NewReader(correct)), nil
	})
	if err != nil {
		t.Fatalf("WriteBlob: unexpected error: %v", err)
	}
	if openCalled != 1 {
		t.Errorf("open() was called %d times, want 1 (size mismatch should trigger rewrite)", openCalled)
	}

	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if !bytes.Equal(got, correct) {
		t.Errorf("blob content = %q, want %q", got, correct)
	}
}

func TestWriteBlob_DigestMismatch(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	actual := []byte("actual content that will be streamed")
	wrongDigest := digest.FromBytes([]byte("this is not the actual content"))

	err = o.WriteBlob(context.Background(), wrongDigest, int64(len(actual)), func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(actual)), nil
	})
	if err == nil {
		t.Fatal("WriteBlob: expected digest mismatch error, got nil")
	}
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("WriteBlob error %v does not wrap ErrDigestMismatch", err)
	}

	blobPath := blobPathFor(dir, wrongDigest)
	if _, statErr := os.Stat(blobPath); !os.IsNotExist(statErr) {
		t.Errorf("final blob path exists after digest mismatch: %v", statErr)
	}

	if n := countTmpFiles(t, dir); n != 0 {
		t.Errorf("left %d temp files behind after digest mismatch, want 0", n)
	}
}

func TestWriteBlob_ConcurrentSameDigest_SingleflightDeduplicates(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := []byte("concurrently written content that many goroutines race to write")
	d := digest.FromBytes(data)

	var opens int32
	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = o.WriteBlob(context.Background(), d, int64(len(data)), func() (io.ReadCloser, error) {
				atomic.AddInt32(&opens, 1)
				return io.NopCloser(bytes.NewReader(data)), nil
			})
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: WriteBlob error: %v", i, e)
		}
	}

	blobPath := blobPathFor(dir, d)
	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}

	if n := countTmpFiles(t, dir); n != 0 {
		t.Errorf("left %d temp files behind, want 0", n)
	}
}

func TestWriteBlob_SeparateOCIInstancesDoNotShareFlights(t *testing.T) {
	// Two Layout/OCI instances pointed at different roots must not share
	// singleflight state -- each is expected to actually invoke open().
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	o1, err := NewOCI(dir1)
	if err != nil {
		t.Fatalf("NewOCI 1: %v", err)
	}
	o2, err := NewOCI(dir2)
	if err != nil {
		t.Fatalf("NewOCI 2: %v", err)
	}

	data := []byte("shared digest content across two independent stores")
	d := digest.FromBytes(data)

	var opens int32
	openFn := func() (io.ReadCloser, error) {
		atomic.AddInt32(&opens, 1)
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	if err := o1.WriteBlob(context.Background(), d, int64(len(data)), openFn); err != nil {
		t.Fatalf("o1.WriteBlob: %v", err)
	}
	if err := o2.WriteBlob(context.Background(), d, int64(len(data)), openFn); err != nil {
		t.Fatalf("o2.WriteBlob: %v", err)
	}

	if opens != 2 {
		t.Errorf("open() called %d times across two independent stores, want 2", opens)
	}
}

// slowChunkedReader hands out data in small fixed-size chunks with a delay
// before each chunk, so that io.Copy has to call Read many times to drain it
// rather than draining it in one shot. This gives a concurrently-running
// context cancellation many chances to be observed by ctxReader between
// chunks, which is what TestWriteBlob_ContextCancellation_AbortsInFlightWrite
// needs to prove cancellation is prompt rather than "eventually noticed on
// the final EOF read".
type slowChunkedReader struct {
	data        []byte
	chunkSize   int
	delay       time.Duration
	onFirstRead func()
	once        sync.Once
}

func (r *slowChunkedReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		if r.onFirstRead != nil {
			r.onFirstRead()
		}
	})
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	n := r.chunkSize
	if n > len(p) {
		n = len(p)
	}
	if n > len(r.data) {
		n = len(r.data)
	}
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

func (r *slowChunkedReader) Close() error { return nil }

// TestWriteBlob_ContextCancellation_AbortsInFlightWrite proves the ctxReader
// wiring actually does something: without it, WriteBlob ignores ctx entirely
// and would run this ~256-chunk, ~750ms streamed write to completion even
// after cancel() fires. With it, the write must stop within a small fraction
// of that time, return an error matching context.Canceled, and leave no
// trace -- neither a final blob nor a leftover temp file -- at the digest's
// path.
func TestWriteBlob_ContextCancellation_AbortsInFlightWrite(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := bytes.Repeat([]byte("x"), 8*1024*1024) // 8MiB
	d := digest.FromBytes(data)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	r := &slowChunkedReader{
		data:        data,
		chunkSize:   32 * 1024, // 256 chunks
		delay:       3 * time.Millisecond,
		onFirstRead: func() { close(started) },
	}

	errCh := make(chan error, 1)
	writeStart := time.Now()
	go func() {
		errCh <- o.WriteBlob(ctx, d, int64(len(data)), func() (io.ReadCloser, error) {
			return r, nil
		})
	}()

	<-started
	cancel()

	var writeErr error
	select {
	case writeErr = <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("WriteBlob never returned after context cancellation")
	}
	elapsed := time.Since(writeStart)

	if !errors.Is(writeErr, context.Canceled) {
		t.Fatalf("WriteBlob error = %v, want context.Canceled", writeErr)
	}
	// The uncancelled write would take ~256*3ms = ~768ms to finish streaming.
	// A prompt abort should return well before that.
	if elapsed > 500*time.Millisecond {
		t.Errorf("WriteBlob took %s to abort after cancellation, want well under the ~768ms an uncancelled write would take", elapsed)
	}

	blobPath := blobPathFor(dir, d)
	if _, statErr := os.Stat(blobPath); !os.IsNotExist(statErr) {
		t.Errorf("final blob path exists after cancellation: stat err = %v", statErr)
	}
	if n := countTmpFiles(t, dir); n != 0 {
		t.Errorf("left %d temp files behind after cancellation, want 0", n)
	}
}

// TestWriteBlob_SemaphoreBoundsConcurrency writes 4x DefaultBlobConcurrency
// distinct digests concurrently -- distinct so none of them hit the fast path
// or dedupe through singleflight -- and has each one's open() track the
// concurrent-in-flight high-water mark. That watermark must never exceed
// consts.DefaultBlobConcurrency, which is only true if WriteBlob's blobSem
// acquire actually bounds the number of writers running at once.
func TestWriteBlob_SemaphoreBoundsConcurrency(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	const n = 4 * consts.DefaultBlobConcurrency
	var inFlight int32
	var maxInFlight int32

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := []byte(fmt.Sprintf("distinct content #%d so every goroutine actually opens", i))
			d := digest.FromBytes(data)
			errs[i] = o.WriteBlob(context.Background(), d, int64(len(data)), func() (io.ReadCloser, error) {
				cur := atomic.AddInt32(&inFlight, 1)
				for {
					old := atomic.LoadInt32(&maxInFlight)
					if cur <= old {
						break
					}
					if atomic.CompareAndSwapInt32(&maxInFlight, old, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&inFlight, -1)
				return io.NopCloser(bytes.NewReader(data)), nil
			})
		}()
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: WriteBlob error: %v", i, e)
		}
	}

	if maxInFlight > consts.DefaultBlobConcurrency {
		t.Errorf("max concurrent opens = %d, want <= %d (consts.DefaultBlobConcurrency)", maxInFlight, consts.DefaultBlobConcurrency)
	}
}

// TestWriteBlob_FastPath_DoesNotAcquireSemaphore saturates blobSem completely
// (acquiring every permit and never releasing) and then calls WriteBlob for a
// digest that already exists on disk. If the fast path acquired a permit
// before returning, this call would block forever behind the saturated
// semaphore; instead it must return immediately without ever calling open().
func TestWriteBlob_FastPath_DoesNotAcquireSemaphore(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := []byte("already present content, the fast path must not touch blobSem")
	d := digest.FromBytes(data)
	blobPath := blobPathFor(dir, d)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(blobPath, data, 0644); err != nil {
		t.Fatalf("pre-writing blob: %v", err)
	}

	for i := 0; i < consts.DefaultBlobConcurrency; i++ {
		if err := o.blobSem.Acquire(context.Background(), 1); err != nil {
			t.Fatalf("saturating blobSem: %v", err)
		}
	}
	// Deliberately never released: any code path in this test that tries to
	// acquire a permit will block for good.

	done := make(chan error, 1)
	go func() {
		done <- o.WriteBlob(context.Background(), d, int64(len(data)), func() (io.ReadCloser, error) {
			return nil, errors.New("open should not have been called on the fast path")
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WriteBlob (fast path, saturated blobSem): unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteBlob (fast path) blocked against a saturated blobSem -- the fast path is acquiring a permit it shouldn't")
	}
}

// gatedReader blocks its first Read call on proceed, then returns all of
// data in one shot once released. This lets a test deterministically control
// when the flight winner's io.Copy loop is unblocked relative to a context
// cancellation, without racing Go's scheduler.
type gatedReader struct {
	data    []byte
	proceed <-chan struct{}
	done    bool
}

func (g *gatedReader) Read(p []byte) (int, error) {
	if g.done {
		return 0, io.EOF
	}
	<-g.proceed
	n := copy(p, g.data)
	g.done = true
	return n, nil
}

func (g *gatedReader) Close() error { return nil }

// TestWriteBlob_SingleflightWinnerCancellation_DoesNotFailIndependentWaiter
// is the acceptance test for the retry fix: goroutine 1 (its own, cancellable
// context) wins the singleflight flight for a shared digest and observes its
// own context's cancellation mid-copy. Goroutine 2 (an independent, never
// -cancelled context) joins the same flight as a waiter. Goroutine 1 must
// fail with context.Canceled; goroutine 2 must still succeed, proving the
// shared flight error was not blindly propagated to a caller whose own
// context was never cancelled.
func TestWriteBlob_SingleflightWinnerCancellation_DoesNotFailIndependentWaiter(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := bytes.Repeat([]byte("shared-digest-content"), 20) // small, fits in one io.Copy buffer read
	d := digest.FromBytes(data)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2 := context.Background() // independent, never cancelled

	openCalled := make(chan struct{})
	proceed := make(chan struct{})
	open1 := func() (io.ReadCloser, error) {
		close(openCalled)
		return &gatedReader{data: data, proceed: proceed}, nil
	}
	open2 := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- o.WriteBlob(ctx1, d, int64(len(data)), open1)
	}()

	select {
	case <-openCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine 1 never became the flight winner (open() was never called)")
	}

	errCh2 := make(chan error, 1)
	go func() {
		// Give goroutine 1 time to register as the singleflight winner before
		// we join as a waiter -- openCalled firing already guarantees this,
		// since singleflight registers the flight before invoking the
		// winner's function.
		errCh2 <- o.WriteBlob(ctx2, d, int64(len(data)), open2)
	}()

	// Let goroutine 2 actually reach o.sf.Do and join the in-flight call.
	time.Sleep(100 * time.Millisecond)

	cancel1()
	close(proceed) // unblock goroutine 1's gatedReader; its next Read observes ctx1 cancellation via ctxReader

	var err1, err2 error
	select {
	case err1 = <-errCh1:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine 1 (flight winner) never returned")
	}
	select {
	case err2 = <-errCh2:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine 2 (independent waiter) never returned")
	}

	if !errors.Is(err1, context.Canceled) {
		t.Errorf("goroutine 1 (flight winner, own ctx cancelled) error = %v, want context.Canceled", err1)
	}
	if err2 != nil {
		t.Errorf("goroutine 2 (independent waiter, own ctx never cancelled) error = %v, want nil", err2)
	}

	blobPath := blobPathFor(dir, d)
	got, readErr := os.ReadFile(blobPath)
	if readErr != nil {
		t.Fatalf("reading blob after retry: %v", readErr)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}
}

func TestOCIPusher_Push_NewBlob_AtomicRenameOnSuccess(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := []byte("manifest-or-layer content pushed via the docker resolver path")
	d := digest.FromBytes(data)

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    d,
		Size:      int64(len(data)),
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	blobPath := filepath.Join(dir, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading committed blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}

	if info, err := os.Stat(blobPath); err != nil {
		t.Fatalf("stat blob: %v", err)
	} else if info.Mode().Perm() != 0644 {
		t.Errorf("blob mode = %o, want 0644", info.Mode().Perm())
	}

	assertNoTmpFiles(t, dir)
}

func TestOCIPusher_Push_DigestMismatch_DoesNotRenameLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	actual := []byte("the actual bytes streamed into the writer")
	wrongDigest := digest.FromBytes([]byte("not the actual bytes"))

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    wrongDigest,
		Size:      int64(len(actual)),
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := w.Write(actual); err != nil {
		t.Fatalf("Write: %v", err)
	}

	closeErr := w.Close()
	if closeErr == nil {
		t.Fatal("Close: expected digest mismatch error, got nil")
	}
	if !strings.Contains(closeErr.Error(), "digest mismatch") {
		t.Errorf("Close error = %v, want it to mention digest mismatch", closeErr)
	}

	blobPath := filepath.Join(dir, ocispec.ImageBlobsDir, wrongDigest.Algorithm().String(), wrongDigest.Hex())
	if _, statErr := os.Stat(blobPath); !os.IsNotExist(statErr) {
		t.Errorf("final blob path exists after digest mismatch: stat err = %v", statErr)
	}

	assertNoTmpFiles(t, dir)
}

func TestOCIPusher_Push_ExistingBlob_ReturnsDiscardWriterAndLeavesBlobUntouched(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	existing := []byte("blob content that already exists on disk")
	d := digest.FromBytes(existing)
	blobPath := filepath.Join(dir, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(blobPath, existing, 0644); err != nil {
		t.Fatalf("pre-writing blob: %v", err)
	}

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    d,
		Size:      int64(len(existing)),
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	// The docker resolver push path always writes the full content even if
	// the pusher reports it already exists; the discard writer must consume
	// it without error and without touching the existing blob.
	if _, err := w.Write(existing); err != nil {
		t.Fatalf("Write to discard writer: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if !bytes.Equal(got, existing) {
		t.Errorf("existing blob was modified: got %q, want %q", got, existing)
	}
}

// TestOCIPusher_Push_SharesBlobConcurrencyBound proves Push acquires against
// the same o.blobSem that content.OCI.WriteBlob does, per the plan's
// requirement that both write paths share a single process-wide ceiling.
// It saturates blobSem, confirms Push for a not-yet-existing blob blocks
// (returning ctx.Err() once ctx is cancelled while queued) rather than
// bypassing the bound, then releases one permit and confirms Push succeeds
// once a slot is free -- and that Close() releases its own permit in turn.
func TestOCIPusher_Push_SharesBlobConcurrencyBound(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	for i := 0; i < consts.DefaultBlobConcurrency; i++ {
		if err := o.blobSem.Acquire(context.Background(), 1); err != nil {
			t.Fatalf("saturating blobSem: %v", err)
		}
	}

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	data := []byte("content pushed while blobSem is fully saturated")
	d := digest.FromBytes(data)
	desc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayer, Digest: d, Size: int64(len(data))}

	// Push must block behind the saturated semaphore: prove it by cancelling
	// a short-lived context while it's queued and confirming Push returns
	// that cancellation rather than silently bypassing the bound.
	shortCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := pusher.Push(shortCtx, desc); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Push against a saturated blobSem with a short-lived context: err = %v, want context.DeadlineExceeded", err)
	}

	// Free exactly one permit, then Push (unbounded ctx this time) must
	// succeed -- proving the earlier block/error was really about the
	// semaphore, not something else broken about Push under saturation.
	o.blobSem.Release(1)

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push after freeing a permit: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close must have released its own permit: acquiring one more should
	// succeed immediately rather than blocking against the still-saturated
	// remainder.
	acquireDone := make(chan error, 1)
	go func() {
		acquireDone <- o.blobSem.Acquire(context.Background(), 1)
	}()
	select {
	case err := <-acquireDone:
		if err != nil {
			t.Fatalf("Acquire after Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blobSem permit was not released by Close")
	}
}

func assertNoTmpFiles(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ocispec.ImageBlobsDir, "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// TestWithBlobConcurrency_OverridesPermitCount constructs an OCI with a
// custom blob concurrency of 2 and asserts exactly 2 permits can be acquired
// without blocking, while a 3rd blocks.
func TestWithBlobConcurrency_OverridesPermitCount(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir, WithBlobConcurrency(2))
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	ctx := context.Background()
	if err := o.blobSem.Acquire(ctx, 1); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := o.blobSem.Acquire(ctx, 1); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		o.blobSem.Acquire(context.Background(), 1) //nolint:errcheck
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("3rd acquire succeeded immediately, want it to block against a 2-permit semaphore")
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}
}

// TestWithBlobConcurrency_ZeroOrNegativeIsNoOp asserts that WithBlobConcurrency
// with n <= 0 leaves the default consts.DefaultBlobConcurrency permit count in
// place, rather than constructing a semaphore with zero (permanently blocked)
// or negative (panicking) capacity.
func TestWithBlobConcurrency_ZeroOrNegativeIsNoOp(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir, WithBlobConcurrency(0))
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	// The default is 16 permits; acquiring one must succeed immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := o.blobSem.Acquire(ctx, 1); err != nil {
		t.Fatalf("acquire against default-capacity semaphore should not block/fail: %v", err)
	}
}

// TestNewOCI_NoOptions_BackwardCompatible asserts the existing no-variadic-arg
// call form still compiles and works after NewOCI gained an opts... parameter.
func TestNewOCI_NoOptions_BackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewOCI(dir); err != nil {
		t.Fatalf("NewOCI(dir) with no options: %v", err)
	}
}

// digestForIndex returns a distinct, syntactically valid sha256 digest for
// index i, so concurrent test goroutines never collide on digest.
func digestForIndex(i int) string {
	return fmt.Sprintf("sha256:%064x", i)
}

// newTestOCI constructs an OCI against dir and loads its index, mirroring
// how store.NewLayout always calls LoadIndex once at construction time.
// AddIndex (both before and after this task's changes) relies on o.index
// already being non-nil -- it does not call LoadIndex itself -- so any test
// that calls AddIndex directly against a bare OCI (bypassing store.Layout)
// must load the index first, exactly like this helper does.
func newTestOCI(t *testing.T, dir string) *OCI {
	t.Helper()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	if err := o.LoadIndex(); err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}
	return o
}

// refDescriptor builds a minimal, valid-for-AddIndex descriptor for index i:
// a distinct tagged reference and a distinct digest.
func refDescriptor(i int) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.Digest(digestForIndex(i)),
		Size:      int64(100 + i),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: fmt.Sprintf("example.com/repo%d:tag%d", i, i),
			consts.KindAnnotationName: consts.KindAnnotationImage,
		},
	}
}

// --------------------------------------------------------------------------
// TestOCI_ConcurrentAddIndex
// --------------------------------------------------------------------------

// TestOCI_ConcurrentAddIndex runs many goroutines each adding a distinct
// descriptor concurrently. It must not panic/race, and -- critically -- a
// *fresh* OCI opened against the same root directory afterward must see all
// entries via LoadIndex. Checking only the original OCI's in-memory nameMap
// would not catch entries lost on disk because one goroutine's nameMap.Range
// snapshot predated another goroutine's Store.
func TestOCI_ConcurrentAddIndex(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	const n = 50
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := o.AddIndex(refDescriptor(i)); err != nil {
				errs <- fmt.Errorf("AddIndex(%d): %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Open a fresh OCI against the same root and confirm all 50 entries
	// survived to disk.
	fresh, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI (fresh): %v", err)
	}
	seen := make(map[string]bool)
	if err := fresh.Walk(func(_ string, d ocispec.Descriptor) error {
		seen[d.Annotations[ocispec.AnnotationRefName]] = true
		return nil
	}); err != nil {
		t.Fatalf("Walk (fresh): %v", err)
	}
	if len(seen) != n {
		t.Fatalf("fresh OCI sees %d entries on disk, want %d", len(seen), n)
	}
	for i := 0; i < n; i++ {
		ref := fmt.Sprintf("example.com/repo%d:tag%d", i, i)
		if !seen[ref] {
			t.Errorf("entry %q missing from disk after concurrent AddIndex", ref)
		}
	}
}

// --------------------------------------------------------------------------
// TestOCI_ConcurrentAddIndexAndWalk
// --------------------------------------------------------------------------

// TestOCI_ConcurrentAddIndexAndWalk runs AddIndex and Walk concurrently.
//
// Without the locking fix in this task, Walk hands out the live descriptor
// (and its live, shared Annotations map) straight out of nameMap while
// AddIndex/SaveIndex concurrently mutate index/nameMap on another goroutine:
// this is `fatal error: concurrent map read and map write`, a hard crash of
// the whole test binary -- not a normal per-test failure. So a green run of
// this specific test (not just "go test reported no failures") is the
// signal that the fix is in place; a regression takes down the process.
func TestOCI_ConcurrentAddIndexAndWalk(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	// Seed a few entries so Walk has something to range over from the start.
	for i := 0; i < 5; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	const n = 50
	var wg sync.WaitGroup

	for i := 5; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = o.AddIndex(refDescriptor(i))
		}(i)
	}

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = o.Walk(func(_ string, d ocispec.Descriptor) error {
				// Read the annotation the way a real Walk caller would; this is
				// the read side of the concurrent read/write hazard.
				_ = d.Annotations[ocispec.AnnotationRefName]
				_ = d.Annotations[consts.KindAnnotationName]
				return nil
			})
		}()
	}

	wg.Wait()
}

// --------------------------------------------------------------------------
// TestOCI_SaveIndexAtomic
// --------------------------------------------------------------------------

// TestOCI_SaveIndexAtomic exercises the on-disk atomicity of SaveIndex's
// write. AddIndex/SaveIndex run concurrently (serialized through the OCI's
// internal lock) while a separate reader repeatedly reads the raw index.json
// bytes directly off disk -- bypassing the OCI's lock entirely, the way a
// second hauler process (no shared in-process mutex) would. A non-atomic
// os.WriteFile would let that reader observe a truncated or partially
// written file; the temp-file+rename approach guarantees the reader only
// ever sees a complete prior version or a complete new version.
func TestOCI_SaveIndexAtomic(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	// Seed so index.json exists before the reader starts.
	if err := o.AddIndex(refDescriptor(0)); err != nil {
		t.Fatalf("seed AddIndex: %v", err)
	}

	indexPath := o.path(ocispec.ImageIndexFile)

	stop := make(chan struct{})
	readErrs := make(chan error, 1)
	var readCount int
	go func() {
		for {
			select {
			case <-stop:
				readErrs <- nil
				return
			default:
			}
			data, err := os.ReadFile(indexPath)
			if err != nil {
				// A concurrent rename can transiently race an Open with ENOENT
				// on some platforms; that's not the hazard under test (torn
				// content), so tolerate it and keep polling.
				continue
			}
			readCount++
			var idx ocispec.Index
			if err := json.Unmarshal(data, &idx); err != nil {
				readErrs <- fmt.Errorf("torn/invalid index.json read: %w (raw: %s)", err, string(data))
				return
			}
		}
	}()

	const n = 50
	var wg sync.WaitGroup
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = o.AddIndex(refDescriptor(i))
		}(i)
	}
	wg.Wait()

	close(stop)
	if err := <-readErrs; err != nil {
		t.Fatal(err)
	}
	if readCount == 0 {
		t.Skip("reader goroutine never observed the file in time; not a meaningful run")
	}
}

// --------------------------------------------------------------------------
// TestOCI_WalkCallbackReenteringResolveDoesNotDeadlock
// --------------------------------------------------------------------------

// TestOCI_WalkCallbackReenteringResolveDoesNotDeadlock is the single most
// important test in this file. It reproduces, at the pkg/content level, the
// exact call shape of store.CopyAll's self-sync path
// (cmd/hauler/cli/store/sync.go:468 calls s.CopyAll(ctx, s.OCI, nil), and
// CopyAll's Walk callback calls l.Copy(ctx, reference, to, toRef) with
// to == l.OCI -- the same OCI instance Walk is iterating). Copy in turn
// calls Resolve, Fetcher, and Pusher on that same OCI.
//
// A naive `sync.Mutex` held across the entire Walk call would deadlock the
// very first time a callback calls back into Resolve/Pusher/LoadIndex on the
// same OCI -- which self-sync does on every normal `store sync` run, not
// just in some edge case. Walk must snapshot under the lock and release
// before invoking any callback.
//
// The whole test is wrapped in a short timeout so that a regression fails
// this test with a clear message instead of hanging `go test` (and CI)
// forever.
func TestOCI_WalkCallbackReenteringResolveDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	for i := 0; i < 3; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- o.Walk(func(key string, d ocispec.Descriptor) error {
			// Re-enter the same OCI instance from inside the Walk callback,
			// exactly as store.Layout.Copy does during self-sync.
			if _, err := o.Resolve(context.Background(), key); err != nil {
				return fmt.Errorf("Resolve reentrant call: %w", err)
			}
			if _, err := o.Fetcher(context.Background(), key); err != nil {
				return fmt.Errorf("Fetcher reentrant call: %w", err)
			}
			if _, err := o.Pusher(context.Background(), key); err != nil {
				return fmt.Errorf("Pusher reentrant call: %w", err)
			}
			if err := o.LoadIndex(); err != nil {
				return fmt.Errorf("LoadIndex reentrant call: %w", err)
			}
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Walk with reentrant callback returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Walk with a callback re-entering Resolve/Fetcher/Pusher/LoadIndex on the same OCI deadlocked (timed out after 5s) -- this is the exact shape of store.CopyAll's self-sync path")
	}
}

// --------------------------------------------------------------------------
// TestOCI_UpdateAnnotations
// --------------------------------------------------------------------------

// TestOCI_UpdateAnnotations covers match+apply semantics: descriptors
// matching the predicate get their annotations replaced via apply, the
// number of matches is returned, and non-matching descriptors are untouched.
func TestOCI_UpdateAnnotations(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	for i := 0; i < 3; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	target := "example.com/repo1:tag1"
	matched, err := o.UpdateAnnotations(
		func(d ocispec.Descriptor) bool {
			return d.Annotations[ocispec.AnnotationRefName] == target
		},
		func(a map[string]string) {
			a[ocispec.AnnotationRefName] = "example.com/repo1:renamed"
		},
	)
	if err != nil {
		t.Fatalf("UpdateAnnotations: %v", err)
	}
	if matched != 1 {
		t.Fatalf("matched = %d, want 1", matched)
	}

	var found bool
	var untouchedCount int
	if err := o.Walk(func(_ string, d ocispec.Descriptor) error {
		ref := d.Annotations[ocispec.AnnotationRefName]
		if ref == "example.com/repo1:renamed" {
			found = true
		}
		if ref == "example.com/repo0:tag0" || ref == "example.com/repo2:tag2" {
			untouchedCount++
		}
		if ref == target {
			t.Errorf("old reference %q still present after UpdateAnnotations", target)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !found {
		t.Error("renamed reference not found after UpdateAnnotations")
	}
	if untouchedCount != 2 {
		t.Errorf("untouched entries = %d, want 2", untouchedCount)
	}
}

// TestOCI_UpdateAnnotationsNoMatchDoesNotWrite verifies that when no
// descriptor matches, UpdateAnnotations returns (0, nil) and does not touch
// index.json on disk at all -- not even a no-op rewrite.
func TestOCI_UpdateAnnotationsNoMatchDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	if err := o.AddIndex(refDescriptor(0)); err != nil {
		t.Fatalf("seed AddIndex: %v", err)
	}

	indexPath := o.path(ocispec.ImageIndexFile)
	old := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(indexPath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	matched, err := o.UpdateAnnotations(
		func(d ocispec.Descriptor) bool { return false },
		func(a map[string]string) { a["should-not-be-called"] = "true" },
	)
	if err != nil {
		t.Fatalf("UpdateAnnotations: %v", err)
	}
	if matched != 0 {
		t.Fatalf("matched = %d, want 0", matched)
	}

	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("index.json mtime changed on a zero-match UpdateAnnotations call: got %v, want %v", info.ModTime(), old)
	}
}

// TestOCI_ConcurrentUpdateAnnotations runs many concurrent UpdateAnnotations
// calls, each targeting a distinct descriptor, and confirms no race/panic
// and that all renames land.
func TestOCI_ConcurrentUpdateAnnotations(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)
	const n = 20
	for i := 0; i < n; i++ {
		if err := o.AddIndex(refDescriptor(i)); err != nil {
			t.Fatalf("seed AddIndex(%d): %v", i, err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			old := fmt.Sprintf("example.com/repo%d:tag%d", i, i)
			_, err := o.UpdateAnnotations(
				func(d ocispec.Descriptor) bool {
					return d.Annotations[ocispec.AnnotationRefName] == old
				},
				func(a map[string]string) {
					a[ocispec.AnnotationRefName] = fmt.Sprintf("example.com/repo%d:renamed", i)
				},
			)
			if err != nil {
				t.Errorf("UpdateAnnotations(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	renamed := make(map[string]bool)
	if err := o.Walk(func(_ string, d ocispec.Descriptor) error {
		renamed[d.Annotations[ocispec.AnnotationRefName]] = true
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("example.com/repo%d:renamed", i)
		if !renamed[want] {
			t.Errorf("missing renamed entry %q after concurrent UpdateAnnotations", want)
		}
	}
}

// --------------------------------------------------------------------------
// TestOCI_AddIndexSkipsSaveWhenUnchanged
// --------------------------------------------------------------------------

// TestOCI_AddIndexSkipsSaveWhenUnchanged verifies the "cheap win" dedup: a
// second AddIndex call with a byte-identical descriptor must not rewrite
// index.json (checked via mtime, since content alone can't distinguish a
// skip from an identical rewrite). A subsequent AddIndex with a genuinely
// different descriptor (different Size) for the same key must still write.
func TestOCI_AddIndexSkipsSaveWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	o := newTestOCI(t, dir)

	desc := refDescriptor(0)
	if err := o.AddIndex(desc); err != nil {
		t.Fatalf("AddIndex (first): %v", err)
	}

	indexPath := o.path(ocispec.ImageIndexFile)
	old := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(indexPath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// Re-add the exact same descriptor (must be treated as unchanged).
	if err := o.AddIndex(desc); err != nil {
		t.Fatalf("AddIndex (repeat, unchanged): %v", err)
	}
	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("index.json was rewritten for a byte-identical AddIndex: mtime got %v, want unchanged %v", info.ModTime(), old)
	}

	// Now add a genuinely different descriptor for the same key (different
	// Size) -- this must write.
	changed := desc
	changed.Size = desc.Size + 1
	if err := o.AddIndex(changed); err != nil {
		t.Fatalf("AddIndex (changed): %v", err)
	}
	info2, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("Stat (after changed): %v", err)
	}
	if info2.ModTime().Equal(old) {
		t.Error("index.json mtime unchanged after a genuinely different AddIndex; expected a write")
	}
}

// addTestIndexEntry adds a uniquely-named descriptor through AddIndex.
func addTestIndexEntry(t *testing.T, o *OCI, i int) {
	t.Helper()
	desc := ocispec.Descriptor{
		MediaType: consts.OCIManifestSchema1,
		Digest:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Size:      1,
		Annotations: map[string]string{
			ocispec.AnnotationRefName: fmt.Sprintf("hauler/test-%d:latest", i),
			consts.KindAnnotationName: consts.KindAnnotationImage,
		},
	}
	if err := o.AddIndex(desc); err != nil {
		t.Fatalf("AddIndex(%d): %v", i, err)
	}
}

func TestAddIndexCheckpointsOnInterval(t *testing.T) {
	// newTestOCI calls LoadIndex, which is required before AddIndex can be
	// called directly against a bare OCI -- see newTestOCI's doc comment in
	// oci_concurrency_test.go for why o.index must be non-nil first.
	o := newTestOCI(t, t.TempDir())

	// Fixed clock: no time passes unless the test advances it.
	clock := time.Now()
	o.now = func() time.Time { return clock }

	for i := 0; i < 25; i++ {
		addTestIndexEntry(t, o, i)
	}

	st := o.Stats().Snapshot()
	if st.IndexWrites != 25 {
		t.Fatalf("IndexWrites = %d, want 25", st.IndexWrites)
	}
	if st.IndexDurableWrites != 1 {
		t.Fatalf("IndexDurableWrites = %d, want 1 (only the first save of a run)", st.IndexDurableWrites)
	}

	// Advance past the interval: the next save must be durable again.
	clock = clock.Add(indexCheckpointInterval + time.Second)
	addTestIndexEntry(t, o, 25)

	st = o.Stats().Snapshot()
	if st.IndexDurableWrites != 2 {
		t.Fatalf("IndexDurableWrites = %d after advancing the clock, want 2", st.IndexDurableWrites)
	}
}

func TestSaveIndexIsAlwaysDurable(t *testing.T) {
	o := newTestOCI(t, t.TempDir())
	clock := time.Now()
	o.now = func() time.Time { return clock }

	for i := 0; i < 3; i++ {
		if err := o.SaveIndex(); err != nil {
			t.Fatalf("SaveIndex: %v", err)
		}
	}

	st := o.Stats().Snapshot()
	if st.IndexDurableWrites != 3 {
		t.Fatalf("IndexDurableWrites = %d, want 3 (SaveIndex ignores the interval)", st.IndexDurableWrites)
	}
}

// TestCheckpointPathsProduceIdenticalIndex verifies the durable and
// non-durable paths differ only in fsync -- the bytes on disk must match.
func TestCheckpointPathsProduceIdenticalIndex(t *testing.T) {
	build := func(durable bool) []byte {
		o := newTestOCI(t, t.TempDir())
		addTestIndexEntry(t, o, 1)
		o.lock()
		if err := o.saveIndexLocked(durable); err != nil {
			o.mu.Unlock()
			t.Fatalf("saveIndexLocked(%v): %v", durable, err)
		}
		o.mu.Unlock()

		data, err := os.ReadFile(o.path(ocispec.ImageIndexFile))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		return data
	}

	if a, b := build(true), build(false); string(a) != string(b) {
		t.Fatalf("durable and non-durable index bytes differ:\n durable: %s\n plain:   %s", a, b)
	}
}
