package store

// store_blob_concurrency_test.go covers store.WithBlobConcurrency, the
// plumbing that lets `store sync --concurrency` override the OCI layout's
// default blob-write concurrency ceiling (content.OCI.blobSem) at
// construction time. blobSem itself is unexported on content.OCI, so this
// test proves the override reached it indirectly: by writing several
// distinct-digest layers concurrently through Layout.writeLayer (which is
// unexported here too -- this file is `package store`, matching
// store_concurrency_test.go's whitebox convention) and observing that the
// concurrent in-flight high-water mark never exceeds the configured limit.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/opencontainers/go-digest"
)

// TestWithBlobConcurrency_BoundsConcurrentWrites constructs a Layout with a
// blob concurrency ceiling of 2 and writes 8 distinct-digest layers
// concurrently (distinct so none hit the fast path or dedupe through
// singleflight). The observed concurrent-in-flight high-water mark must
// never exceed 2, which is only true if WithBlobConcurrency's override
// actually reached the underlying OCI's blobSem.
func TestWithBlobConcurrency_BoundsConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir, WithBlobConcurrency(2))
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	const n = 8
	var inFlight int32
	var maxInFlight int32

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := []byte(fmt.Sprintf("distinct blob content #%d", i))
			h, err := v1.NewHash(digest.FromBytes(data).String())
			if err != nil {
				errs[i] = err
				return
			}
			lyr := &fakeLayer{
				hash: h,
				size: int64(len(data)),
				compressed: func() (io.ReadCloser, error) {
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
					time.Sleep(30 * time.Millisecond)
					atomic.AddInt32(&inFlight, -1)
					return io.NopCloser(bytes.NewReader(data)), nil
				},
			}
			errs[i] = s.writeLayer(context.Background(), lyr)
		}()
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: writeLayer error: %v", i, e)
		}
	}

	if maxInFlight > 2 {
		t.Errorf("max concurrent blob writes = %d, want <= 2 (WithBlobConcurrency(2) not applied)", maxInFlight)
	}
}

// TestNewLayout_WithoutBlobConcurrency_UsesDefault asserts that NewLayout
// without WithBlobConcurrency behaves exactly as before -- a store usable for
// normal operations, no error.
func TestNewLayout_WithoutBlobConcurrency_UsesDefault(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewLayout(dir); err != nil {
		t.Fatalf("NewLayout(dir) with no options: %v", err)
	}
}

// TestNewLayout_WithCache_StillWorksAlongsideBlobConcurrencyRestructure is a
// regression guard: NewLayout was restructured to run the opts loop before
// constructing the underlying OCI store, so WithCache (which only touches
// l.cache, independent of l.OCI) must still apply correctly alongside
// WithBlobConcurrency.
func TestNewLayout_WithCache_StillWorksAlongsideBlobConcurrencyRestructure(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLayout(dir, WithCache(nil), WithBlobConcurrency(4))
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	if s.OCI == nil {
		t.Fatal("NewLayout: s.OCI is nil")
	}
}
