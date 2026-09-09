package archives

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestArchive writes size bytes of pseudo-random content to path and returns that content.
func newTestArchive(t *testing.T, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("failed to generate test data: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write test archive: %v", err)
	}
	return data
}

func TestSplitArchiveRedundant_RoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	want := newTestArchive(t, archivePath, 10_000)

	chunks, err := SplitArchiveRedundant(ctx, archivePath, 1_500, 25)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple shards, got %d", len(chunks))
	}

	joined, err := JoinChunks(ctx, chunks[0], t.TempDir())
	if err != nil {
		t.Fatalf("JoinChunks failed: %v", err)
	}
	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatalf("failed to read joined archive: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("joined archive does not match original content")
	}
}

// TestSplitArchiveRedundant_RepairsMissingShards deletes shards up to the parity budget and verifies the archive still reassembles correctly.
func TestSplitArchiveRedundant_RepairsMissingShards(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	want := newTestArchive(t, archivePath, 10_000)

	chunks, err := SplitArchiveRedundant(ctx, archivePath, 1_500, 25)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}

	header, ok, err := isRedundantChunkSet(chunks)
	if err != nil || !ok {
		t.Fatalf("expected a redundant chunk set, ok=%v err=%v", ok, err)
	}
	parity := int(header.parityShards)
	if parity < 1 {
		t.Fatalf("expected at least 1 parity shard, got %d", parity)
	}

	// delete exactly `parity` shards, the maximum this set can tolerate
	for i := 0; i < parity; i++ {
		if err := os.Remove(chunks[i]); err != nil {
			t.Fatalf("failed to delete shard for test: %v", err)
		}
	}

	joined, err := JoinChunks(ctx, chunks[parity], t.TempDir())
	if err != nil {
		t.Fatalf("JoinChunks failed to repair from parity: %v", err)
	}
	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatalf("failed to read joined archive: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("repaired archive does not match original content")
	}
}

// TestSplitArchiveRedundant_RepairsCorruptedShards flips bytes in shards (rather than deleting them) and verifies the checksum catches the damage and parity repairs it.
func TestSplitArchiveRedundant_RepairsCorruptedShards(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	want := newTestArchive(t, archivePath, 10_000)

	chunks, err := SplitArchiveRedundant(ctx, archivePath, 1_500, 25)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}

	if _, ok, err := isRedundantChunkSet(chunks); err != nil || !ok {
		t.Fatalf("expected a redundant chunk set, ok=%v err=%v", ok, err)
	}

	// corrupt one shard's payload in place, leaving its size and header untouched
	corrupted := chunks[0]
	data, err := os.ReadFile(corrupted)
	if err != nil {
		t.Fatalf("failed to read shard to corrupt: %v", err)
	}
	if len(data) <= redundancyHeaderSize {
		t.Fatalf("shard [%s] too small to corrupt meaningfully", corrupted)
	}
	data[redundancyHeaderSize] ^= 0xFF
	if err := os.WriteFile(corrupted, data, 0o644); err != nil {
		t.Fatalf("failed to write corrupted shard: %v", err)
	}

	joined, err := JoinChunks(ctx, chunks[1], t.TempDir())
	if err != nil {
		t.Fatalf("JoinChunks failed to repair corrupted shard: %v", err)
	}
	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatalf("failed to read joined archive: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Error("repaired archive does not match original content after corruption")
	}
}

// TestSplitArchiveRedundant_TooManyLosses verifies a clear error, not silent corruption, when more shards are missing than parity can cover.
func TestSplitArchiveRedundant_TooManyLosses(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	newTestArchive(t, archivePath, 10_000)

	chunks, err := SplitArchiveRedundant(ctx, archivePath, 1_500, 25)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}

	header, ok, err := isRedundantChunkSet(chunks)
	if err != nil || !ok {
		t.Fatalf("expected a redundant chunk set, ok=%v err=%v", ok, err)
	}

	// delete one more shard than parity can tolerate
	tooMany := int(header.parityShards) + 1
	for i := 0; i < tooMany; i++ {
		if err := os.Remove(chunks[i]); err != nil {
			t.Fatalf("failed to delete shard for test: %v", err)
		}
	}

	if _, err := JoinChunks(ctx, chunks[tooMany], t.TempDir()); err == nil {
		t.Fatal("expected an error when losses exceed the parity budget, got nil")
	}
}

// TestSplitArchiveRedundant_TotalShardsOverLimit confirms a combined data+parity total over 256 is rejected with a clear error, not the raw reedsolomon one, even when the data shard count alone is within bounds.
func TestSplitArchiveRedundant_TotalShardsOverLimit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	newTestArchive(t, archivePath, 200)

	_, err := SplitArchiveRedundant(ctx, archivePath, 1, 50)
	if err == nil {
		t.Fatal("expected an error when data+parity shards exceed 256, got nil")
	}
	if !strings.Contains(err.Error(), "256 shard limit") {
		t.Fatalf("expected a clear 256-shard-limit error, got: %v", err)
	}
}

// TestSplitArchiveRedundant_ZeroPercentStillGeneratesOneParityShard confirms a low, non-zero redundancy request still produces at least one parity shard.
func TestSplitArchiveRedundant_ZeroPercentStillGeneratesOneParityShard(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	newTestArchive(t, archivePath, 10_000)

	chunks, err := SplitArchiveRedundant(ctx, archivePath, 1_500, 1)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}

	header, ok, err := isRedundantChunkSet(chunks)
	if err != nil || !ok {
		t.Fatalf("expected a redundant chunk set, ok=%v err=%v", ok, err)
	}
	if header.parityShards < 1 {
		t.Errorf("expected at least 1 parity shard for a non-zero redundancy request, got %d", header.parityShards)
	}
}
