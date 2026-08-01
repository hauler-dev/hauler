package store

import (
	"strings"
	"testing"
	"time"

	"hauler.dev/go/hauler/v2/pkg/content"
)

func TestFormatIOStats(t *testing.T) {
	st := content.IOStatsSnapshot{
		BlobsWritten:       387,
		BlobsCached:        25,
		BlobBytesWritten:   8_100_000_000,
		BlobSemWait:        41200 * time.Millisecond,
		BlobPeakInFlight:   18,
		IndexWrites:        226,
		IndexDurableWrites: 7,
		IndexBytesWritten:  10_300_000,
		IndexLockWait:      3100 * time.Millisecond,
	}

	// BlobPeakInFlight (18) and the ceiling argument (20) are deliberately
	// distinct: if formatIOStats's Sprintf ever transposed the two %d
	// operands, "peak-inflight=18/20" would fail even though both values
	// individually appear elsewhere in the format string. A fixture where
	// both were 20 could not catch that swap.
	got := formatIOStats(st, 20)

	for _, want := range []string{
		"blobs=412",
		"written=387",
		"cached=25",
		"peak-inflight=18/20",
		"blobsem-wait=41.2s",
		"index-writes=226",
		"durable=7",
		"index-lock-wait=3.1s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatIOStats output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("formatIOStats must produce a single line, got:\n%s", got)
	}
}
