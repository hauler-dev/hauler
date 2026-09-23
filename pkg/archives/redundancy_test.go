package archives

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/log"
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

	if ok, err := isRedundantChunkSet(chunks); err != nil || !ok {
		t.Fatalf("expected a redundant chunk set, ok=%v err=%v", ok, err)
	}
	header, _, err := readShardHeader(chunks[0])
	if err != nil {
		t.Fatalf("readShardHeader: %v", err)
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

	if _, ok, err := readShardHeader(chunks[0]); err != nil || !ok {
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

	header, ok, err := readShardHeader(chunks[0])
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

	// One payload byte per shard keeps 200 data shards within bounds while 50% parity pushes the total to 300.
	_, err := SplitArchiveRedundant(ctx, archivePath, redundancyHeaderSize+1, 50)
	if err == nil {
		t.Fatal("expected an error when data+parity shards exceed 256, got nil")
	}
	if !strings.Contains(err.Error(), "256 chunk limit") {
		t.Fatalf("expected a clear 256-chunk-limit error, got: %v", err)
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

	header, ok, err := readShardHeader(chunks[0])
	if err != nil || !ok {
		t.Fatalf("expected a redundant chunk set, ok=%v err=%v", ok, err)
	}
	if header.parityShards < 1 {
		t.Errorf("expected at least 1 parity shard for a non-zero redundancy request, got %d", header.parityShards)
	}
}

// joinAndRead reassembles the chunk set containing chunk and returns the result.
func joinAndRead(t *testing.T, chunk string) []byte {
	t.Helper()
	joined, err := JoinChunks(context.Background(), chunk, t.TempDir())
	if err != nil {
		t.Fatalf("JoinChunks failed: %v", err)
	}
	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatalf("failed to read joined archive: %v", err)
	}
	return got
}

// TestSplitArchiveRedundant_HeaderDamageTreatedAsDamaged is a regression test: a later shard whose header claims an earlier slot used to pass its payload-only checksum and derail repair.
func TestSplitArchiveRedundant_HeaderDamageTreatedAsDamaged(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	want := newTestArchive(t, archivePath, 10_000)
	chunks, err := SplitArchiveRedundant(context.Background(), archivePath, 2_000, 20)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}

	// Shard index sits at header bytes 20-21; point chunk 3 at chunk 2's slot.
	f, err := os.OpenFile(chunks[2], os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{0, 1}, 20); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if got := joinAndRead(t, chunks[0]); !bytes.Equal(got, want) {
		t.Error("archive with a damaged shard header did not reassemble to the original")
	}
}

// TestReconstructAndJoin_VerifiesRepairedSet confirms a shard with a valid checksum but wrong content is caught by the post-repair Verify instead of being joined.
func TestReconstructAndJoin_VerifiesRepairedSet(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	newTestArchive(t, archivePath, 10_000)
	chunks, err := SplitArchiveRedundant(context.Background(), archivePath, 2_000, 20)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}

	// Forge chunk 2: same header, different payload, freshly computed checksum.
	h, _, err := readShardHeader(chunks[1])
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(chunks[1])
	payload := make([]byte, info.Size()-redundancyHeaderSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	sum := newShardCRC(h)
	sum.Write(payload)
	h.crc32 = sum.Sum32()
	if err := os.WriteFile(chunks[1], append(h.encode(), payload...), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force a repair so the forged shard feeds into reconstruction.
	if err := os.Remove(chunks[3]); err != nil {
		t.Fatal(err)
	}

	_, err = JoinChunks(context.Background(), chunks[0], t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "failed verification") {
		t.Fatalf("expected a verification error, got: %v", err)
	}
}

// TestSplitArchiveRedundant_ShardsFitChunkSize covers the worst case, an archive that's an exact multiple of the chunk size, where the header used to push each shard past it.
func TestSplitArchiveRedundant_ShardsFitChunkSize(t *testing.T) {
	const maxBytes = 1_024
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	want := newTestArchive(t, archivePath, 4*maxBytes)
	chunks, err := SplitArchiveRedundant(context.Background(), archivePath, maxBytes, 25)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}
	for _, c := range chunks {
		if info, err := os.Stat(c); err != nil || info.Size() > maxBytes {
			t.Errorf("%s is %d bytes, over the %d byte chunk size (err %v)", filepath.Base(c), info.Size(), maxBytes, err)
		}
	}
	if got := joinAndRead(t, chunks[0]); !bytes.Equal(got, want) {
		t.Error("joined archive does not match original content")
	}
}

// TestSplitArchives_RemoveLeftoverChunks confirms both split modes clear higher-numbered chunks from an earlier, larger save, so load never mixes the two.
func TestSplitArchives_RemoveLeftoverChunks(t *testing.T) {
	split := map[string]func(ctx context.Context, path string) ([]string, error){
		"plain": func(ctx context.Context, path string) ([]string, error) { return SplitArchive(ctx, path, 4_000) },
		"redundant": func(ctx context.Context, path string) ([]string, error) {
			return SplitArchiveRedundant(ctx, path, 4_000, 25)
		},
	}
	for name, fn := range split {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, "haul.tar.zst")
			for i := 1; i <= 9; i++ {
				if err := os.WriteFile(fmt.Sprintf("%s.%03d", archivePath, i), []byte("stale chunk from an older save"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			unrelated := filepath.Join(dir, "haul.tar.zst.notes")
			if err := os.WriteFile(unrelated, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			want := newTestArchive(t, archivePath, 10_000)

			chunks, err := fn(context.Background(), archivePath)
			if err != nil {
				t.Fatalf("split failed: %v", err)
			}
			left, _ := filepath.Glob(archivePath + ".[0-9][0-9][0-9]")
			if len(left) != len(chunks) {
				t.Errorf("expected only the %d new chunks on disk, found %d: %v", len(chunks), len(left), left)
			}
			if _, err := os.Stat(unrelated); err != nil {
				t.Errorf("a file that isn't a chunk was removed: %v", err)
			}
			if got := joinAndRead(t, chunks[0]); !bytes.Equal(got, want) {
				t.Error("joined archive does not match original content")
			}
		})
	}
}

// TestSplitArchiveRedundant_NoTempFiles confirms shards are written in place, leaving nothing but the chunks behind.
func TestSplitArchiveRedundant_NoTempFiles(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	newTestArchive(t, archivePath, 10_000)
	chunks, err := SplitArchiveRedundant(context.Background(), archivePath, 2_000, 20)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != len(chunks) {
		t.Errorf("expected only %d chunks in the output dir, found %d entries", len(chunks), len(entries))
	}
}

// TestReconstructAndJoin_SkipsRepairWhenOnlyParityMissing confirms losing parity alone joins directly from the intact data shards, without a repair pass.
func TestReconstructAndJoin_SkipsRepairWhenOnlyParityMissing(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	want := newTestArchive(t, archivePath, 10_000)
	chunks, err := SplitArchiveRedundant(context.Background(), archivePath, 2_000, 20)
	if err != nil {
		t.Fatalf("SplitArchiveRedundant failed: %v", err)
	}
	// Parity shards are written last, so the final chunk is always parity.
	if err := os.Remove(chunks[len(chunks)-1]); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	ctx := log.NewLogger(&logs).WithContext(context.Background())
	joined, err := JoinChunks(ctx, chunks[0], t.TempDir())
	if err != nil {
		t.Fatalf("JoinChunks failed: %v", err)
	}
	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("joined archive does not match original content")
	}
	if strings.Contains(logs.String(), "recovering") || !strings.Contains(logs.String(), "no repair needed") {
		t.Errorf("expected the repair pass to be skipped, logs:\n%s", logs.String())
	}
}
