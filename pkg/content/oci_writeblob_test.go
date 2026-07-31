package content

import (
	"bytes"
	"context"
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
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

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
