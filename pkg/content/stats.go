package content

import (
	"sync/atomic"
	"time"
)

// IOStats accumulates disk-contention counters for a single OCI store, so
// one pasted log line explains a field performance report -- the previous
// parallel-blob work was abandoned partly because regression reports were
// unreproducible with the reporter's contention invisible. Counters live on
// OCI rather than being context-attached (the store.WithImageStats idiom)
// because they measure store-level semaphore/mutex contention, not
// per-image state like ImageStats. All fields are atomic since every
// increment runs concurrently by design.
type IOStats struct {
	BlobsWritten       atomic.Int64
	BlobsCached        atomic.Int64
	BlobBytesWritten   atomic.Int64
	BlobSemWaitNanos   atomic.Int64
	BlobPeakInFlight   atomic.Int64
	IndexWrites        atomic.Int64
	IndexDurableWrites atomic.Int64
	IndexBytesWritten  atomic.Int64
	IndexLockWaitNanos atomic.Int64

	// blobInFlight backs BlobPeakInFlight. It is never reported: the
	// instantaneous value at the end of a run is always 0 and says nothing.
	blobInFlight atomic.Int64
}

// IOStatsSnapshot is a plain-value copy of IOStats, so formatting code never
// touches atomics and can be unit-tested with a literal.
type IOStatsSnapshot struct {
	BlobsWritten       int64
	BlobsCached        int64
	BlobBytesWritten   int64
	BlobSemWait        time.Duration
	BlobPeakInFlight   int64
	IndexWrites        int64
	IndexDurableWrites int64
	IndexBytesWritten  int64
	IndexLockWait      time.Duration
}

// Snapshot reads every counter. It is not atomic as a whole -- counters are
// read one at a time and may skew relative to each other if a sync is still
// running. Callers report it after all work has finished, where that does
// not matter.
func (s *IOStats) Snapshot() IOStatsSnapshot {
	return IOStatsSnapshot{
		BlobsWritten:       s.BlobsWritten.Load(),
		BlobsCached:        s.BlobsCached.Load(),
		BlobBytesWritten:   s.BlobBytesWritten.Load(),
		BlobSemWait:        time.Duration(s.BlobSemWaitNanos.Load()),
		BlobPeakInFlight:   s.BlobPeakInFlight.Load(),
		IndexWrites:        s.IndexWrites.Load(),
		IndexDurableWrites: s.IndexDurableWrites.Load(),
		IndexBytesWritten:  s.IndexBytesWritten.Load(),
		IndexLockWait:      time.Duration(s.IndexLockWaitNanos.Load()),
	}
}

// enterBlob records that a blob write has just acquired a permit, updating
// the high-water mark with a CAS loop. Must be paired with exitBlob.
func (s *IOStats) enterBlob() {
	cur := s.blobInFlight.Add(1)
	for {
		peak := s.BlobPeakInFlight.Load()
		if cur <= peak || s.BlobPeakInFlight.CompareAndSwap(peak, cur) {
			return
		}
	}
}

// exitBlob records that a blob write has released its permit.
func (s *IOStats) exitBlob() {
	s.blobInFlight.Add(-1)
}

// addSemWait records time spent blocked acquiring blobSem.
func (s *IOStats) addSemWait(d time.Duration) {
	s.BlobSemWaitNanos.Add(int64(d))
}
