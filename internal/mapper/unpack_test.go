package mapper

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
)

func TestStripTopLevel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "nested path", in: "mydir/sub/file.txt", want: "sub/file.txt"},
		{name: "top-level entry only", in: "mydir", want: ""},
		{name: "no separator at all", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripTopLevel(tt.in); got != tt.want {
				t.Errorf("stripTopLevel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// newArchiveFixture builds a tar+gzip archive shaped like tarDir's output and returns its bytes.
func newArchiveFixture(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)

	if err := tw.WriteHeader(&tar.Header{Name: prefix, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatalf("failed to write archive dir header: %v", err)
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fileContent := files[name]
		hdr := &tar.Header{Name: prefix + "/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(fileContent))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write archive header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(fileContent)); err != nil {
			t.Fatalf("failed to write archive content for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

func TestExtractArchive(t *testing.T) {
	files := map[string]string{
		"a.txt":        "hello",
		"nested/b.txt": "world",
	}
	archiveBytes := newArchiveFixture(t, "mydir", files)

	archivePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(archivePath, archiveBytes, 0o644); err != nil {
		t.Fatalf("failed to write archive fixture: %v", err)
	}

	dir := t.TempDir()
	if err := extractArchive(archivePath, dir); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("failed to read extracted %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "mydir")); !os.IsNotExist(err) {
		t.Errorf("expected the archive's top-level prefix directory to be stripped, but %s exists", filepath.Join(dir, "mydir"))
	}
}

// TestPush_UnpacksDirectoryContent verifies an AnnotationUnpack descriptor expands into a directory, replacing whatever stale content was already there.
func TestPush_UnpacksDirectoryContent(t *testing.T) {
	files := map[string]string{
		"a.txt":        "hello",
		"nested/b.txt": "world",
	}
	archiveBytes := newArchiveFixture(t, "mydir", files)
	dgst := digest.FromBytes(archiveBytes)

	root := t.TempDir()
	s, err := NewMapperFileStore(root, Files())
	if err != nil {
		t.Fatalf("failed to create mapper file store: %v", err)
	}
	p, err := s.Pusher(context.Background(), "test:latest")
	if err != nil {
		t.Fatalf("failed to create pusher: %v", err)
	}

	// Stale content already at the destination should be replaced, not merged with.
	stalePath := filepath.Join(root, "mydir")
	if err := os.MkdirAll(stalePath, 0o755); err != nil {
		t.Fatalf("failed to seed stale directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stalePath, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("failed to seed stale file: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: consts.FileLayerMediaType,
		Digest:    dgst,
		Size:      int64(len(archiveBytes)),
		Annotations: map[string]string{
			ocispec.AnnotationTitle:  "mydir",
			content.AnnotationUnpack: "true",
		},
	}

	w, err := p.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if _, err := w.Write(archiveBytes); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(stalePath, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("failed to read extracted %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	if _, err := os.Stat(filepath.Join(stalePath, "stale.txt")); !os.IsNotExist(err) {
		t.Error("expected stale.txt to be removed, but it still exists")
	}
}

// TestPush_RawFileUnaffected is a regression guard: ordinary file content should still be written as-is.
func TestPush_RawFileUnaffected(t *testing.T) {
	fileBytes := []byte("hello world")
	dgst := digest.FromBytes(fileBytes)

	root := t.TempDir()
	s, err := NewMapperFileStore(root, Files())
	if err != nil {
		t.Fatalf("failed to create mapper file store: %v", err)
	}
	p, err := s.Pusher(context.Background(), "test:latest")
	if err != nil {
		t.Fatalf("failed to create pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		Digest: dgst,
		Size:   int64(len(fileBytes)),
		Annotations: map[string]string{
			ocispec.AnnotationTitle: "hello.txt",
		},
	}

	w, err := p.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}
	if _, err := w.Write(fileBytes); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != string(fileBytes) {
		t.Errorf("hello.txt = %q, want %q", got, fileBytes)
	}
}
