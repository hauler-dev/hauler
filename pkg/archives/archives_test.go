package archives

import (
	"bytes"
	"context"
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
		wantExt   string
		wantIndex int
		wantOk    bool
	}{
		{
			name:      "compound extension",
			path:      "/tmp/haul_3.tar.zst",
			wantBase:  "/tmp/haul",
			wantExt:   ".tar.zst",
			wantIndex: 3,
			wantOk:    true,
		},
		{
			name:      "single extension",
			path:      "/tmp/archive_0.zst",
			wantBase:  "/tmp/archive",
			wantExt:   ".zst",
			wantIndex: 0,
			wantOk:    true,
		},
		{
			name:      "large index",
			path:      "/tmp/haul_42.tar.zst",
			wantBase:  "/tmp/haul",
			wantExt:   ".tar.zst",
			wantIndex: 42,
			wantOk:    true,
		},
		{
			name:   "no numeric suffix",
			path:   "/tmp/haul.tar.zst",
			wantOk: false,
		},
		{
			name:   "alphabetic suffix",
			path:   "/tmp/haul_abc.tar.zst",
			wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, ext, index, ok := chunkInfo(tt.path)
			if ok != tt.wantOk {
				t.Fatalf("chunkInfo() ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if base != tt.wantBase {
				t.Errorf("chunkInfo() base = %q, want %q", base, tt.wantBase)
			}
			if ext != tt.wantExt {
				t.Errorf("chunkInfo() ext = %q, want %q", ext, tt.wantExt)
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

			// chunks must follow <base>_N<ext> naming
			for i, chunk := range chunks {
				expected := filepath.Join(dir, fmt.Sprintf("haul_%d.tar.zst", i))
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
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("haul_%d.tar.zst", i)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		got, err := JoinChunks(ctx, filepath.Join(dir, "haul_0.tar.zst"), tempDir)
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
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("data_%d.tar.zst", i)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// pass chunk_1, not chunk_0 — should still assemble from chunk_0
		got, err := JoinChunks(ctx, filepath.Join(dir, "data_1.tar.zst"), tempDir)
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
		if err := os.WriteFile(filepath.Join(dir, "haul_0.tar.zst"), []byte("valid"), 0o644); err != nil {
			t.Fatal(err)
		}
		// glob matches this but chunkInfo rejects it
		if err := os.WriteFile(filepath.Join(dir, "haul_foo.tar.zst"), []byte("invalid"), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := JoinChunks(ctx, filepath.Join(dir, "haul_0.tar.zst"), tempDir)
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
