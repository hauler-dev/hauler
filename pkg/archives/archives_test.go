package archives

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mholt/archives"
	"github.com/rs/zerolog"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	l := zerolog.New(io.Discard)
	return l.WithContext(context.Background())
}

func TestArchive_RoundTrip(t *testing.T) {
	ctx := testContext(t)

	srcDir := t.TempDir()
	files := map[string]string{
		"file1.txt":         "hello world",
		"subdir/file2.txt":  "nested content",
		"subdir/file3.json": `{"key":"value"}`,
	}
	for relPath, content := range files {
		full := filepath.Join(srcDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("create parent dir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", relPath, err)
		}
	}

	outFile := filepath.Join(t.TempDir(), "test.tar.zst")
	if err := Archive(ctx, srcDir, outFile, archives.Zstd{}, archives.Tar{}); err != nil {
		t.Fatalf("Archive() error: %v", err)
	}

	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("archive file missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("archive file is empty")
	}

	dstDir := t.TempDir()
	if err := Unarchive(ctx, outFile, dstDir); err != nil {
		t.Fatalf("Unarchive() error: %v", err)
	}

	// Archive maps files under the source directory's base name.
	baseName := filepath.Base(srcDir)
	for relPath, expectedContent := range files {
		full := filepath.Join(dstDir, baseName, relPath)
		data, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("read extracted file %s: %v", relPath, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("content mismatch for %s: got %q, want %q", relPath, string(data), expectedContent)
		}
	}
}

func TestArchive_NonExistentDir(t *testing.T) {
	ctx := testContext(t)
	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	outFile := filepath.Join(t.TempDir(), "out.tar.zst")
	if err := Archive(ctx, nonExistent, outFile, archives.Zstd{}, archives.Tar{}); err == nil {
		t.Fatal("Archive() should return an error for a non-existent source directory")
	}
}

func TestUnarchive_ExistingHaul(t *testing.T) {
	ctx := testContext(t)

	// testdata/ is two levels up from pkg/archives/
	haulPath := filepath.Join("..", "..", "testdata", "haul.tar.zst")
	if _, err := os.Stat(haulPath); err != nil {
		t.Skipf("testdata/haul.tar.zst not found at %s: %v", haulPath, err)
	}

	dstDir := t.TempDir()
	if err := Unarchive(ctx, haulPath, dstDir); err != nil {
		t.Fatalf("Unarchive() error: %v", err)
	}

	var indexPath string
	if err := filepath.Walk(dstDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == "index.json" {
			indexPath = path
		}
		return nil
	}); err != nil {
		t.Fatalf("walk extracted dir: %v", err)
	}
	if indexPath == "" {
		t.Fatal("index.json not found in extracted haul archive")
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("index.json is not valid JSON")
	}
}

func TestSecurePath(t *testing.T) {
	basePath := "/tmp/extract"

	tests := []struct {
		name         string
		relativePath string
		wantResult   string
	}{
		{
			name:         "normal relative path",
			relativePath: "subdir/file.txt",
			wantResult:   "/tmp/extract/subdir/file.txt",
		},
		{
			name:         "simple filename",
			relativePath: "readme.txt",
			wantResult:   "/tmp/extract/readme.txt",
		},
		// Path traversal attempts are sanitized (not rejected): "/../../../etc/passwd"
		// cleans to "/etc/passwd", strips leading "/" → "etc/passwd", joined → base/etc/passwd.
		{
			name:         "path traversal is sanitized to safe path",
			relativePath: "../../../etc/passwd",
			wantResult:   "/tmp/extract/etc/passwd",
		},
		{
			name:         "deeply nested traversal is sanitized",
			relativePath: "a/b/../../../../etc/shadow",
			wantResult:   "/tmp/extract/etc/shadow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := securePath(basePath, tt.relativePath)
			if err != nil {
				t.Fatalf("securePath(%q, %q) unexpected error: %v", basePath, tt.relativePath, err)
			}
			if result != tt.wantResult {
				t.Errorf("securePath(%q, %q) = %q, want %q", basePath, tt.relativePath, result, tt.wantResult)
			}
		})
	}
}

// --------------------------------------------------------------------------
// chunkInfo
// --------------------------------------------------------------------------

func TestChunkInfo(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantBase  string
		wantIndex int
		wantOk    bool
	}{
		{
			name:      "compound extension",
			path:      "/tmp/haul.tar.zst.003",
			wantBase:  "/tmp/haul.tar.zst",
			wantIndex: 3,
			wantOk:    true,
		},
		{
			name:      "single extension",
			path:      "/tmp/archive.zst.001",
			wantBase:  "/tmp/archive.zst",
			wantIndex: 1,
			wantOk:    true,
		},
		{
			name:      "large index",
			path:      "/tmp/haul.tar.zst.042",
			wantBase:  "/tmp/haul.tar.zst",
			wantIndex: 42,
			wantOk:    true,
		},
		{
			name:      "beyond 3-digit padding",
			path:      "/tmp/haul.tar.zst.1000",
			wantBase:  "/tmp/haul.tar.zst",
			wantIndex: 1000,
			wantOk:    true,
		},
		{
			name:   "no numeric suffix",
			path:   "/tmp/haul.tar.zst",
			wantOk: false,
		},
		{
			name:   "alphabetic suffix",
			path:   "/tmp/haul.tar.zst.abc",
			wantOk: false,
		},
		{
			name:   "short numeric suffix rejected",
			path:   "/tmp/report.v1.2",
			wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, index, ok := chunkInfo(tt.path)
			if ok != tt.wantOk {
				t.Fatalf("chunkInfo() ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if base != tt.wantBase {
				t.Errorf("chunkInfo() base = %q, want %q", base, tt.wantBase)
			}
			if index != tt.wantIndex {
				t.Errorf("chunkInfo() index = %d, want %d", index, tt.wantIndex)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SplitArchive
// --------------------------------------------------------------------------

func TestSplitArchive(t *testing.T) {
	ctx := testContext(t)

	tests := []struct {
		name     string
		dataSize int
		maxBytes int64
	}{
		{name: "splits into multiple chunks", dataSize: 100, maxBytes: 30},
		{name: "single chunk when data fits", dataSize: 50, maxBytes: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, "haul.tar.zst")
			data := make([]byte, tt.dataSize)
			for i := range data {
				data[i] = byte(i % 256)
			}
			if err := os.WriteFile(archivePath, data, 0o644); err != nil {
				t.Fatal(err)
			}

			chunks, err := SplitArchive(ctx, archivePath, tt.maxBytes)
			if err != nil {
				t.Fatalf("SplitArchive() error = %v", err)
			}
			if len(chunks) == 0 {
				t.Fatal("SplitArchive() returned no chunks")
			}

			// original archive must be removed
			if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
				t.Error("original archive should be removed after splitting")
			}

			// chunks must follow <archivePath>.NNN naming (3-digit, 1-based)
			for i, chunk := range chunks {
				expected := filepath.Join(dir, fmt.Sprintf("haul.tar.zst.%03d", i+1))
				if chunk != expected {
					t.Errorf("chunk[%d] = %s, want %s", i, chunk, expected)
				}
			}

			// concatenating chunks must reproduce the original data
			var combined []byte
			for _, chunk := range chunks {
				b, err := os.ReadFile(chunk)
				if err != nil {
					t.Fatal(err)
				}
				combined = append(combined, b...)
			}
			if !bytes.Equal(combined, data) {
				t.Error("combined chunks do not match original data")
			}
		})
	}
}

func TestSplitArchive_MissingFile(t *testing.T) {
	ctx := testContext(t)
	dir := t.TempDir()
	_, err := SplitArchive(ctx, filepath.Join(dir, "nonexistent.tar.zst"), 1<<30)
	if err == nil {
		t.Fatal("SplitArchive() expected error for missing file, got nil")
	}
}

// --------------------------------------------------------------------------
// JoinChunks
// --------------------------------------------------------------------------

func TestJoinChunks(t *testing.T) {
	ctx := testContext(t)

	t.Run("joins multiple chunks in order", func(t *testing.T) {
		dir := t.TempDir()
		tempDir := t.TempDir()
		for i, content := range []string{"chunk0-data", "chunk1-data", "chunk2-data"} {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("haul.tar.zst.%03d", i+1)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		got, err := JoinChunks(ctx, filepath.Join(dir, "haul.tar.zst.001"), tempDir)
		if err != nil {
			t.Fatalf("JoinChunks() error = %v", err)
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte("chunk0-datachunk1-datachunk2-data"); !bytes.Equal(data, want) {
			t.Errorf("JoinChunks() content = %q, want %q", data, want)
		}
	})

	t.Run("any chunk triggers full assembly", func(t *testing.T) {
		dir := t.TempDir()
		tempDir := t.TempDir()
		for i, content := range []string{"aaa", "bbb"} {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("data.tar.zst.%03d", i+1)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// pass chunk .002, not .001... should still assemble from .001
		got, err := JoinChunks(ctx, filepath.Join(dir, "data.tar.zst.002"), tempDir)
		if err != nil {
			t.Fatalf("JoinChunks() error = %v", err)
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte("aaabbb"); !bytes.Equal(data, want) {
			t.Errorf("JoinChunks() content = %q, want %q", data, want)
		}
	})

	t.Run("non-chunk file returned unchanged", func(t *testing.T) {
		dir := t.TempDir()
		nonChunk := filepath.Join(dir, "haul.tar.zst")
		if err := os.WriteFile(nonChunk, []byte("not-a-chunk"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := JoinChunks(ctx, nonChunk, t.TempDir())
		if err != nil {
			t.Fatalf("JoinChunks() error = %v", err)
		}
		if got != nonChunk {
			t.Errorf("JoinChunks() = %s, want %s (unchanged)", got, nonChunk)
		}
	})

	t.Run("non-numeric suffix files excluded", func(t *testing.T) {
		dir := t.TempDir()
		tempDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "haul.tar.zst.001"), []byte("valid"), 0o644); err != nil {
			t.Fatal(err)
		}
		// glob matches this but chunkInfo rejects it
		if err := os.WriteFile(filepath.Join(dir, "haul.tar.zst.foo"), []byte("invalid"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := JoinChunks(ctx, filepath.Join(dir, "haul.tar.zst.001"), tempDir)
		if err != nil {
			t.Fatalf("JoinChunks() error = %v", err)
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, []byte("valid")) {
			t.Errorf("JoinChunks() included non-numeric suffix file; content = %q", data)
		}
	})
}

// --------------------------------------------------------------------------
// ArchiveFiles
// --------------------------------------------------------------------------

func TestArchiveFiles(t *testing.T) {
	ctx := context.Background()

	// A "store" dir with a nested blobs tree, and a separate scratch dir whose
	// generated index.json must land at the archive root in place of the store's.
	storeDir := t.TempDir()
	blobDir := filepath.Join(storeDir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, "aaa"), []byte("blob-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "index.json"), []byte(`{"stale":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratch, "index.json"), []byte(`{"generated":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "out.tar.zst")
	files := map[string]string{
		filepath.Join(storeDir, "blobs"):     "blobs",
		filepath.Join(scratch, "index.json"): "index.json",
	}
	if err := ArchiveFiles(ctx, files, out, CompressionMap["zst"], ArchivalMap["tar"]); err != nil {
		t.Fatalf("ArchiveFiles: %v", err)
	}

	dest := t.TempDir()
	if err := Unarchive(ctx, out, dest); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "index.json"))
	if err != nil {
		t.Fatalf("index.json missing from archive root: %v", err)
	}
	if string(got) != `{"generated":true}` {
		t.Errorf("index.json = %s, want the scratch copy, not the store's", got)
	}
	if _, err := os.ReadFile(filepath.Join(dest, "blobs", "sha256", "aaa")); err != nil {
		t.Errorf("blobs tree not nested correctly: %v", err)
	}
}

// TestArchiveFiles_DeterministicOrder guards against mholt/archives.FilesFromDisk's
// map-range iteration (randomized per range, even within one process) leaking into
// tar entry order: two ArchiveFiles runs over the identical multi-entry fileMap must
// produce byte-identical archives, or identical saves would diverge and
// --chunk-size boundaries would shift between runs (#744 fix 2).
func TestArchiveFiles_DeterministicOrder(t *testing.T) {
	ctx := context.Background()

	srcDir := t.TempDir()
	entries := map[string]string{
		"alpha.txt":          "alpha-content",
		"bravo.txt":          "bravo-content",
		"charlie/nested.txt": "charlie-content",
		"delta.txt":          "delta-content",
		"echo.txt":           "echo-content",
	}
	for rel, content := range entries {
		full := filepath.Join(srcDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	fileMap := map[string]string{
		filepath.Join(srcDir, "alpha.txt"): "alpha.txt",
		filepath.Join(srcDir, "bravo.txt"): "bravo.txt",
		filepath.Join(srcDir, "charlie"):   "charlie",
		filepath.Join(srcDir, "delta.txt"): "delta.txt",
		filepath.Join(srcDir, "echo.txt"):  "echo.txt",
	}

	var digests [2]string
	for i := range digests {
		out := filepath.Join(t.TempDir(), "out.tar.zst")
		if err := ArchiveFiles(ctx, fileMap, out, CompressionMap["zst"], ArchivalMap["tar"]); err != nil {
			t.Fatalf("ArchiveFiles run %d: %v", i, err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read archive run %d: %v", i, err)
		}
		digests[i] = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	if digests[0] != digests[1] {
		t.Errorf("archive digests differ across identical runs: %s vs %s", digests[0], digests[1])
	}
}

// --------------------------------------------------------------------------
// SplitArchive + JoinChunks round-trip
// --------------------------------------------------------------------------

func TestSplitJoinChunks_RoundTrip(t *testing.T) {
	ctx := testContext(t)

	original := make([]byte, 1000)
	for i := range original {
		original[i] = byte(i % 256)
	}

	dir := t.TempDir()
	archivePath := filepath.Join(dir, "haul.tar.zst")
	if err := os.WriteFile(archivePath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	chunks, err := SplitArchive(ctx, archivePath, 100)
	if err != nil {
		t.Fatalf("SplitArchive() error = %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("SplitArchive() returned no chunks")
	}

	joined, err := JoinChunks(ctx, chunks[0], t.TempDir())
	if err != nil {
		t.Fatalf("JoinChunks() error = %v", err)
	}

	got, err := os.ReadFile(joined)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Error("round-trip: joined data does not match original")
	}
}
