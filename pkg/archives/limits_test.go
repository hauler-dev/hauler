package archives

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func limitsTestContext(t *testing.T) context.Context {
	t.Helper()
	l := zerolog.New(io.Discard)
	return l.WithContext(context.Background())
}

// buildSmallTarZst creates a real .tar.zst in a temp dir using the production
// Archive() function, so its format is always compatible with Unarchive().
func buildSmallTarZst(t *testing.T, entries map[string]string) string {
	t.Helper()
	srcDir := t.TempDir()
	for name, body := range entries {
		full := filepath.Join(srcDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "test.tar.zst")
	ctx := limitsTestContext(t)
	if err := Archive(ctx, srcDir, out, compressionZstd, archivalTar); err != nil {
		t.Fatalf("Archive(): %v", err)
	}
	return out
}

// TestUnarchive_PerFileByteCap verifies that a single file exceeding
// maxArchiveFileBytes is rejected during extraction.
func TestUnarchive_PerFileByteCap(t *testing.T) {
	body := make([]byte, 512)
	archive := buildSmallTarZst(t, map[string]string{"big.bin": string(body)})

	dst := t.TempDir()
	ctx := limitsTestContext(t)
	limits := extractionLimits{
		maxFileBytes:  256, // smaller than the 512-byte file
		maxTotalBytes: 100 << 30,
		maxFiles:      100_000,
	}
	if err := unarchiveWithLimits(ctx, archive, dst, limits); err == nil {
		t.Fatal("unarchiveWithLimits() expected error for per-file cap, got nil")
	}
}

// TestUnarchive_AggregateByteCap verifies that the total extracted bytes across
// all files cannot exceed maxTotalBytes.
func TestUnarchive_AggregateByteCap(t *testing.T) {
	// Two files each 256 bytes — aggregate cap set to 400, which is exceeded.
	body := make([]byte, 256)
	archive := buildSmallTarZst(t, map[string]string{
		"a.bin": string(body),
		"b.bin": string(body),
	})

	dst := t.TempDir()
	ctx := limitsTestContext(t)
	limits := extractionLimits{
		maxFileBytes:  100 << 30, // per-file: no constraint
		maxTotalBytes: 400,       // aggregate: 256+256 = 512 > 400
		maxFiles:      100_000,
	}
	if err := unarchiveWithLimits(ctx, archive, dst, limits); err == nil {
		t.Fatal("unarchiveWithLimits() expected error for aggregate cap, got nil")
	}
}

// TestUnarchive_FileCountCap verifies that an archive with more entries than
// maxFiles is rejected.
func TestUnarchive_FileCountCap(t *testing.T) {
	entries := make(map[string]string, 6)
	for i := 0; i < 6; i++ {
		entries[filepath.Join("subdir", string(rune('a'+i))+".txt")] = "x"
	}
	archive := buildSmallTarZst(t, entries)

	dst := t.TempDir()
	ctx := limitsTestContext(t)
	limits := extractionLimits{
		maxFileBytes:  100 << 30,
		maxTotalBytes: 100 << 30,
		maxFiles:      3, // only 3 allowed; archive has 6
	}
	if err := unarchiveWithLimits(ctx, archive, dst, limits); err == nil {
		t.Fatal("unarchiveWithLimits() expected error for file-count cap, got nil")
	}
}

// TestUnarchive_WithinLimits confirms the default path succeeds for normal archives.
func TestUnarchive_WithinLimits(t *testing.T) {
	archive := buildSmallTarZst(t, map[string]string{
		"file1.txt": "hello",
		"file2.txt": "world",
	})
	dst := t.TempDir()
	ctx := limitsTestContext(t)
	if err := Unarchive(ctx, archive, dst); err != nil {
		t.Fatalf("Unarchive() unexpected error: %v", err)
	}
}

// TestUnarchive_DecompressionRatioCap verifies that an archive whose
// decompressed:compressed ratio exceeds consts.MaxDecompressionRatio is
// rejected during extraction.  Highly redundant content (10 MiB of zero bytes)
// compresses with zstd to a few KiB, well above the 100x default ratio.
func TestUnarchive_DecompressionRatioCap(t *testing.T) {
	body := make([]byte, 10<<20) // 10 MiB of zero bytes
	archive := buildSmallTarZst(t, map[string]string{"compressible.bin": string(body)})

	stat, err := os.Stat(archive)
	if err != nil {
		t.Fatalf("stat archive: %v", err)
	}
	t.Logf("archive size: %d bytes (decompressed: %d bytes, ratio: %.0fx)",
		stat.Size(), len(body), float64(len(body))/float64(stat.Size()))

	dst := t.TempDir()
	ctx := limitsTestContext(t)
	limits := extractionLimits{
		maxFileBytes:  100 << 30, // not the constraint
		maxTotalBytes: 100 << 30, // not the constraint
		maxFiles:      100_000,
	}
	err = unarchiveWithLimits(ctx, archive, dst, limits)
	if err == nil {
		t.Fatal("unarchiveWithLimits() expected error for decompression ratio, got nil")
	}
	if !strings.Contains(err.Error(), "decompression ratio") {
		t.Errorf("expected error mentioning decompression ratio, got: %v", err)
	}
}
