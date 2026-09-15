package content

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
)

// writeTestBlob writes content through WriteBlob and returns its digest.
func writeTestBlob(t *testing.T, o *OCI, body string) digest.Digest {
	t.Helper()
	dg := digest.FromString(body)
	err := o.WriteBlob(context.Background(), dg, int64(len(body)), func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	return dg
}

func TestIOStatsCountsWrittenAndCached(t *testing.T) {
	o, err := NewOCI(t.TempDir())
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	writeTestBlob(t, o, "hello world")

	st := o.Stats().Snapshot()
	if st.BlobsWritten != 1 {
		t.Fatalf("BlobsWritten = %d, want 1", st.BlobsWritten)
	}
	if st.BlobsCached != 0 {
		t.Fatalf("BlobsCached = %d, want 0", st.BlobsCached)
	}
	if st.BlobBytesWritten != int64(len("hello world")) {
		t.Fatalf("BlobBytesWritten = %d, want %d", st.BlobBytesWritten, len("hello world"))
	}

	// Same digest again: must hit the os.Stat fast path, not rewrite.
	writeTestBlob(t, o, "hello world")

	st = o.Stats().Snapshot()
	if st.BlobsWritten != 1 {
		t.Fatalf("BlobsWritten = %d after rewrite, want 1", st.BlobsWritten)
	}
	if st.BlobsCached != 1 {
		t.Fatalf("BlobsCached = %d, want 1", st.BlobsCached)
	}
	if st.BlobBytesWritten != int64(len("hello world")) {
		t.Fatalf("BlobBytesWritten = %d after cache hit, want unchanged", st.BlobBytesWritten)
	}
}

// TestBlobCountersCreditEveryCallerOnSharedDigest is a regression test for a bug where a singleflight follower's counters stayed uncredited since only the leader's ctx ever ran.
func TestBlobCountersCreditEveryCallerOnSharedDigest(t *testing.T) {
	o, err := NewOCI(t.TempDir())
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	body := "shared blob content"
	dg := digest.FromString(body)

	started := make(chan struct{})
	proceed := make(chan struct{})
	var openOnce sync.Once
	open := func() (io.ReadCloser, error) {
		openOnce.Do(func() { close(started) })
		<-proceed
		return io.NopCloser(strings.NewReader(body)), nil
	}

	var statsA, statsB struct {
		Cached, Written atomic.Int64
	}
	ctxA := WithBlobCounters(context.Background(), &statsA.Cached, &statsA.Written)
	ctxB := WithBlobCounters(context.Background(), &statsB.Cached, &statsB.Written)

	var wg sync.WaitGroup
	var errA, errB error
	wg.Add(1)
	go func() {
		defer wg.Done()
		errA = o.WriteBlob(ctxA, dg, int64(len(body)), open)
	}()

	<-started // A is now blocked in open(), holding the flight open

	wg.Add(1)
	go func() {
		defer wg.Done()
		errB = o.WriteBlob(ctxB, dg, int64(len(body)), open)
	}()

	// Give B's goroutine time to join A's flight as a follower, same pattern as TestIOStatsRecordsLockWait.
	time.Sleep(50 * time.Millisecond)
	close(proceed)
	wg.Wait()

	if errA != nil {
		t.Fatalf("WriteBlob (A): %v", errA)
	}
	if errB != nil {
		t.Fatalf("WriteBlob (B): %v", errB)
	}

	if st := o.Stats().Snapshot(); st.BlobsWritten != 1 {
		t.Fatalf("BlobsWritten = %d, want 1 (only one real write for the shared digest)", st.BlobsWritten)
	}

	if total := statsA.Cached.Load() + statsA.Written.Load(); total != 1 {
		t.Errorf("caller A's counters totaled %d, want 1", total)
	}
	if total := statsB.Cached.Load() + statsB.Written.Load(); total != 1 {
		t.Errorf("caller B's counters totaled %d, want 1 (a follower's per-operation counters must be credited too)", total)
	}
}

func TestIOStatsPeakInFlightNeverExceedsCeiling(t *testing.T) {
	const ceiling = 3
	o, err := NewOCI(t.TempDir(), WithBlobConcurrency(ceiling))
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	if got := o.BlobConcurrency(); got != ceiling {
		t.Fatalf("BlobConcurrency() = %d, want %d", got, ceiling)
	}

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := strings.Repeat("x", 1024) + string(rune('a'+i%26)) + strings.Repeat("y", i)
			dg := digest.FromString(body)
			_ = o.WriteBlob(context.Background(), dg, int64(len(body)), func() (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(body)), nil
			})
		}(i)
	}
	wg.Wait()

	st := o.Stats().Snapshot()
	if st.BlobPeakInFlight > ceiling {
		t.Fatalf("BlobPeakInFlight = %d, must never exceed the ceiling of %d", st.BlobPeakInFlight, ceiling)
	}
	if st.BlobPeakInFlight < 1 {
		t.Fatalf("BlobPeakInFlight = %d, expected at least 1 concurrent write to be observed", st.BlobPeakInFlight)
	}
}

func TestIOStatsCountsIndexWrites(t *testing.T) {
	o, err := NewOCI(t.TempDir())
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	// o.index is only populated by LoadIndex; SaveIndex on a bare OCI (no
	// prior LoadIndex) is a nil-pointer panic by design -- see newTestOCI's
	// doc comment in oci_concurrency_test.go. Mirror that same pattern here.
	if err := o.LoadIndex(); err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if err := o.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	st := o.Stats().Snapshot()
	if st.IndexWrites != 1 {
		t.Fatalf("IndexWrites = %d, want 1", st.IndexWrites)
	}
	if st.IndexBytesWritten <= 0 {
		t.Fatalf("IndexBytesWritten = %d, want > 0", st.IndexBytesWritten)
	}
}

func TestIOStatsRecordsLockWait(t *testing.T) {
	o, err := NewOCI(t.TempDir())
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	// See TestIOStatsCountsIndexWrites: o.index must be populated before
	// SaveIndex can run, so load it before we grab o.mu below.
	if err := o.LoadIndex(); err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	// Hold the mutex directly so the next lock() call must block on it.
	o.mu.Lock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = o.SaveIndex()
	}()

	// Give the goroutine time to reach lock() and block there.
	time.Sleep(50 * time.Millisecond)
	o.mu.Unlock()
	<-done

	st := o.Stats().Snapshot()
	if st.IndexLockWait < 10*time.Millisecond {
		t.Fatalf("IndexLockWait = %v, want at least 10ms of recorded contention", st.IndexLockWait)
	}
}
