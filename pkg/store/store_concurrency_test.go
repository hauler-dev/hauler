package store

// store_concurrency_test.go covers the atomic/verified/deduplicated blob
// write path (writeLayer -> content.OCI.WriteBlob). This file is
// intentionally `package store` (whitebox) rather than `package store_test`
// because writeLayer is unexported.

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

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/content"
)

// fakeLayer is a hand-rolled v1.Layer that can lie about its digest -- useful
// for the digest-mismatch tests where static.NewLayer (which always computes
// its digest correctly from the given bytes) can't be used.
type fakeLayer struct {
	hash       v1.Hash
	size       int64
	compressed func() (io.ReadCloser, error)
}

func (f *fakeLayer) Digest() (v1.Hash, error)             { return f.hash, nil }
func (f *fakeLayer) DiffID() (v1.Hash, error)             { return f.hash, nil }
func (f *fakeLayer) Size() (int64, error)                 { return f.size, nil }
func (f *fakeLayer) MediaType() (types.MediaType, error)  { return types.OCILayer, nil }
func (f *fakeLayer) Compressed() (io.ReadCloser, error)   { return f.compressed() }
func (f *fakeLayer) Uncompressed() (io.ReadCloser, error) { return f.compressed() }

var _ v1.Layer = (*fakeLayer)(nil)

// stallingReader delays its first Read call so that concurrent goroutines
// racing to write the same digest have a chance to actually overlap (join
// the same singleflight flight) before the winner finishes streaming.
type stallingReader struct {
	r     io.Reader
	once  sync.Once
	delay time.Duration
}

func (s *stallingReader) Read(p []byte) (int, error) {
	s.once.Do(func() { time.Sleep(s.delay) })
	return s.r.Read(p)
}

func (s *stallingReader) Close() error { return nil }

func blobPathForTest(root string, alg, hex string) string {
	return filepath.Join(root, ocispec.ImageBlobsDir, alg, hex)
}

func countTmpFilesInStore(t *testing.T, root string) int {
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

// TestWriteLayer_ConcurrentSameDigest writes the same digest through 16
// concurrent goroutines using a stalling reader so the goroutines actually
// overlap. It asserts the final blob hashes correctly and that no leftover
// temp files remain.
func TestWriteLayer_ConcurrentSameDigest(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	data := []byte("layer content shared by many concurrent writers for digest dedup test")
	h, err := v1.NewHash(digest.FromBytes(data).String())
	if err != nil {
		t.Fatalf("NewHash: %v", err)
	}

	var opens int32
	lyr := &fakeLayer{
		hash: h,
		size: int64(len(data)),
		compressed: func() (io.ReadCloser, error) {
			atomic.AddInt32(&opens, 1)
			return &stallingReader{r: bytes.NewReader(data), delay: 30 * time.Millisecond}, nil
		},
	}

	const n = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.writeLayer(context.Background(), lyr)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: writeLayer error: %v", i, e)
		}
	}

	blobPath := blobPathForTest(dir, h.Algorithm, h.Hex)
	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}

	if tmp := countTmpFilesInStore(t, dir); tmp != 0 {
		t.Errorf("left %d temp files behind, want 0", tmp)
	}
}

// TestWriteLayer_DigestMismatch uses a layer whose Digest() lies about its
// content. writeLayer must return an error wrapping content.ErrDigestMismatch,
// must not create the final blob path, and must not leave temp files behind.
func TestWriteLayer_DigestMismatch(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	actual := []byte("this is the actual streamed content")
	wrongDigestStr := digest.FromBytes([]byte("this is not the actual content")).String()
	h, err := v1.NewHash(wrongDigestStr)
	if err != nil {
		t.Fatalf("NewHash: %v", err)
	}

	lyr := &fakeLayer{
		hash: h,
		size: int64(len(actual)),
		compressed: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(actual)), nil
		},
	}

	err = s.writeLayer(context.Background(), lyr)
	if err == nil {
		t.Fatal("writeLayer: expected digest mismatch error, got nil")
	}
	if !errors.Is(err, content.ErrDigestMismatch) {
		t.Errorf("writeLayer error %v does not wrap content.ErrDigestMismatch", err)
	}

	blobPath := blobPathForTest(dir, h.Algorithm, h.Hex)
	if _, statErr := os.Stat(blobPath); !os.IsNotExist(statErr) {
		t.Errorf("final blob path exists after digest mismatch: stat err = %v", statErr)
	}

	if tmp := countTmpFilesInStore(t, dir); tmp != 0 {
		t.Errorf("left %d temp files behind after digest mismatch, want 0", tmp)
	}
}

// TestWriteLayer_DoesNotDeletePeerBlob directly targets the removed
// store.go:899 `os.Remove(blobPath)` bug: a peer writes digest X
// successfully and commits it to disk; a second, independent write for the
// SAME digest X is then deliberately made to fail (it declares a different
// size, so the fast path can't skip it, and streams content that doesn't
// hash to X). The already-committed blob at X must survive the second
// writer's failure untouched.
func TestWriteLayer_DoesNotDeletePeerBlob(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	good := []byte("the correct content that a peer successfully commits for digest X")
	h, err := v1.NewHash(digest.FromBytes(good).String())
	if err != nil {
		t.Fatalf("NewHash: %v", err)
	}

	goodLayer := &fakeLayer{
		hash: h,
		size: int64(len(good)),
		compressed: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(good)), nil
		},
	}
	if err := s.writeLayer(context.Background(), goodLayer); err != nil {
		t.Fatalf("peer writeLayer (good): unexpected error: %v", err)
	}

	// A second, independent layer claims the SAME digest X but streams
	// unrelated content of a different length -- this forces the fast path
	// (which only compares sizes) to miss and re-enter the write path, where
	// the digest check must fail without touching the peer's committed blob.
	badContent := []byte("totally different content, different length, will not hash to X")
	failingLayer := &fakeLayer{
		hash: h,
		size: int64(len(badContent)),
		compressed: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(badContent)), nil
		},
	}

	err = s.writeLayer(context.Background(), failingLayer)
	if err == nil {
		t.Fatal("writeLayer (failing peer): expected error, got nil")
	}
	if !errors.Is(err, content.ErrDigestMismatch) {
		t.Errorf("writeLayer (failing peer) error %v does not wrap content.ErrDigestMismatch", err)
	}

	// The peer's originally-committed blob must be untouched.
	blobPath := blobPathForTest(dir, h.Algorithm, h.Hex)
	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob after failing peer write: %v", err)
	}
	if !bytes.Equal(got, good) {
		t.Errorf("peer blob was corrupted/deleted: got %q, want %q", got, good)
	}
}

// TestWriteLayer_TruncatedBlobDetectedAndRewritten pre-places a short/corrupt
// file directly at a blob's final path (bypassing writeLayer entirely, as a
// crash mid-download would leave behind), then calls writeLayer for that same
// digest with the correct known size. The fast path must NOT trust the
// truncated file; it must detect the size mismatch and rewrite the blob
// correctly.
func TestWriteLayer_TruncatedBlobDetectedAndRewritten(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	data := []byte("this is the correct, full-length layer content for the truncation test")
	lyr := static.NewLayer(data, types.OCILayer)
	d, err := lyr.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}

	blobPath := blobPathForTest(dir, d.Algorithm, d.Hex)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(blobPath, []byte("short"), 0644); err != nil {
		t.Fatalf("pre-writing truncated blob: %v", err)
	}

	if err := s.writeLayer(context.Background(), lyr); err != nil {
		t.Fatalf("writeLayer: unexpected error: %v", err)
	}

	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q (truncated blob was not detected/rewritten)", got, data)
	}
}

// TestWriteImageBlobs_FailureCancelsSiblings is the acceptance test for the
// errgroup.Group -> errgroup.WithContext conversion in writeImageBlobs: on a
// zero-value errgroup.Group, Wait() returns the first error but never
// cancels the group's derived context, so every other layer's writeLayer
// call would run to completion regardless of the failure. It builds one
// image (via mutate.AppendLayers over empty.Image, which lets fakeLayer
// stand in as a v1.Layer without hand-rolling the rest of the v1.Image
// interface) with several slow "good" layers and one layer whose open()
// fails immediately, then asserts that not every good layer's blob actually
// landed on disk -- proof that the group's context was cancelled and
// propagated into content.OCI.WriteBlob before all of them finished.
func TestWriteImageBlobs_FailureCancelsSiblings(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	const nGood = 8
	var layers []v1.Layer
	var goodHashes []v1.Hash

	// Each good layer streams its content in small delayed chunks so that
	// the group's context -- cancelled almost immediately by the failing
	// layer below -- has many chances to be observed mid-copy, the same
	// reasoning as content's slowChunkedReader.
	for i := 0; i < nGood; i++ {
		data := bytes.Repeat([]byte(fmt.Sprintf("g%d", i)), 200*1024) // ~400KB+, distinct per layer
		h, err := v1.NewHash(digest.FromBytes(data).String())
		if err != nil {
			t.Fatalf("NewHash: %v", err)
		}
		goodHashes = append(goodHashes, h)
		layers = append(layers, &fakeLayer{
			hash: h,
			size: int64(len(data)),
			compressed: func() (io.ReadCloser, error) {
				return &slowChunkReader{data: data, chunk: 32 * 1024, delay: 5 * time.Millisecond}, nil
			},
		})
	}

	// The failing layer's open() errors out immediately -- no delay -- so it
	// wins the race to fail and cancel the group's context well before the
	// slow good layers finish streaming.
	badData := []byte("this layer's open always fails")
	badHash, err := v1.NewHash(digest.FromBytes(badData).String())
	if err != nil {
		t.Fatalf("NewHash: %v", err)
	}
	layers = append(layers, &fakeLayer{
		hash: badHash,
		size: int64(len(badData)),
		compressed: func() (io.ReadCloser, error) {
			return nil, errors.New("simulated layer fetch failure")
		},
	})

	img, err := mutate.AppendLayers(empty.Image, layers...)
	if err != nil {
		t.Fatalf("mutate.AppendLayers: %v", err)
	}

	err = s.writeImageBlobs(context.Background(), img)
	if err == nil {
		t.Fatal("writeImageBlobs: expected an error from the failing layer, got nil")
	}

	completed := 0
	for _, h := range goodHashes {
		blobPath := blobPathForTest(dir, h.Algorithm, h.Hex)
		if _, statErr := os.Stat(blobPath); statErr == nil {
			completed++
		}
	}
	if completed == nGood {
		t.Errorf("all %d good layers completed despite a sibling failure -- errgroup did not cancel them (zero-value errgroup.Group regression)", nGood)
	}
}

// slowChunkReader hands out data in small fixed-size chunks with a delay
// before each one, giving a concurrently cancelled context many chances to
// be observed between chunks rather than requiring the whole blob to stream
// before cancellation is noticed.
type slowChunkReader struct {
	data  []byte
	chunk int
	delay time.Duration
}

func (r *slowChunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	n := r.chunk
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

func (r *slowChunkReader) Close() error { return nil }
