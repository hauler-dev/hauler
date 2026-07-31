package store

import (
	"strings"
	"testing"
	"time"

	"github.com/dustin/go-humanize"

	"hauler.dev/go/hauler/v2/pkg/store"
)

// TestFormatAddedLine_NilStats proves formatAddedLine never prints
// "0 layers" when stats is nil, and produces a sane elapsed-only line.
func TestFormatAddedLine_NilStats(t *testing.T) {
	got := formatAddedLine("example.com/repo:v1", nil, 1500*time.Millisecond)

	if strings.Contains(got, "0 layer") {
		t.Errorf("formatAddedLine with nil stats must never print \"0 layers\", got %q", got)
	}
	if !strings.Contains(got, "example.com/repo:v1") {
		t.Errorf("formatAddedLine must include the ref, got %q", got)
	}
	if !strings.Contains(got, "1.5s") {
		t.Errorf("formatAddedLine must include the elapsed time as %%.1fs, got %q", got)
	}
	if !strings.HasPrefix(got, "✓ added") {
		t.Errorf("formatAddedLine must start with \"✓ added\", got %q", got)
	}
}

// TestFormatAddedLine_ZeroValueStats proves formatAddedLine treats a
// zero-value *store.ImageStats (Layers == 0) the same as nil: elapsed-only,
// never "0 layers".
func TestFormatAddedLine_ZeroValueStats(t *testing.T) {
	stats := &store.ImageStats{}
	got := formatAddedLine("example.com/repo:v1", stats, 2*time.Second)

	if strings.Contains(got, "0 layer") {
		t.Errorf("formatAddedLine with zero-value stats must never print \"0 layers\", got %q", got)
	}
	if !strings.Contains(got, "2.0s") {
		t.Errorf("formatAddedLine must include the elapsed time as %%.1fs, got %q", got)
	}
}

// TestFormatAddedLine_WithStats proves formatAddedLine includes layer
// count (correctly pluralized), human-readable byte size, and elapsed time
// when stats has at least one layer.
func TestFormatAddedLine_WithStats(t *testing.T) {
	tests := []struct {
		name    string
		layers  int64
		bytes   int64
		wantSub string
	}{
		{name: "singular layer", layers: 1, bytes: 100, wantSub: "1 layer,"},
		{name: "plural layers", layers: 3, bytes: 1_500_000, wantSub: "3 layers,"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &store.ImageStats{}
			stats.Layers.Store(tt.layers)
			stats.Bytes.Store(tt.bytes)

			got := formatAddedLine("example.com/repo:v1", stats, 3*time.Second)

			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("formatAddedLine = %q, want substring %q", got, tt.wantSub)
			}
			wantSize := humanize.Bytes(uint64(tt.bytes))
			if !strings.Contains(got, wantSize) {
				t.Errorf("formatAddedLine = %q, want it to contain human size %q", got, wantSize)
			}
			if !strings.Contains(got, "3.0s") {
				t.Errorf("formatAddedLine = %q, want it to contain elapsed \"3.0s\"", got)
			}
		})
	}
}
