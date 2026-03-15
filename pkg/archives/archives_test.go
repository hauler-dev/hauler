package archives

import (
	"context"
	"encoding/json"
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
