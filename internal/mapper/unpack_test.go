package mapper

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/containerd/containerd/v2/core/remotes"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/getter"
)

func TestEntryTarget(t *testing.T) {
	root := filepath.FromSlash("/dest")
	tests := map[string]string{
		"mydir/sub/file.txt": filepath.FromSlash("/dest/sub/file.txt"),
		"mydir":              root,
		"mydir/":             root,
		"mydir/../x":         filepath.FromSlash("/x"),
	}
	for in, want := range tests {
		if got := entryTarget(root, in); got != want {
			t.Errorf("entryTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// newArchiveFixture builds a tar+gzip archive shaped like tarDir's output and returns its bytes.
func newArchiveFixture(t *testing.T, prefix string, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := []archiveEntry{dirEntry(prefix)}
	for _, name := range names {
		entries = append(entries, fileEntry(prefix+"/"+name, files[name]))
	}
	return tarGz(t, entries)
}

func TestExtractArchive(t *testing.T) {
	files := map[string]string{
		"a.txt":        "hello",
		"nested/b.txt": "world",
	}
	archivePath := archiveFile(t, newArchiveFixture(t, "mydir", files))

	dir := t.TempDir()
	if err := extractArchive(context.Background(), archivePath, dir); err != nil {
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

// archiveEntry is one tar header plus optional body for tarGz.
type archiveEntry struct {
	hdr  tar.Header
	body string
}

// tarGz writes entries, in order, into a tar+gzip archive and returns its bytes.
func tarGz(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		hdr := e.hdr
		hdr.Size = int64(len(e.body))
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("failed to write header for %s: %v", hdr.Name, err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatalf("failed to write body for %s: %v", hdr.Name, err)
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

// buildArchive writes entries into a tar+gzip file and returns its path.
func buildArchive(t *testing.T, entries []archiveEntry) string {
	t.Helper()
	return archiveFile(t, tarGz(t, entries))
}

// archiveFile writes archive bytes to a temp file and returns its path.
func archiveFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("failed to write archive fixture: %v", err)
	}
	return path
}

// newTestPusher returns a Files mapper pusher that extracts into root.
func newTestPusher(t *testing.T, root string) remotes.Pusher {
	t.Helper()
	s, err := NewMapperFileStore(root, Files())
	if err != nil {
		t.Fatalf("failed to create mapper file store: %v", err)
	}
	p, err := s.Pusher(context.Background(), "test:latest")
	if err != nil {
		t.Fatalf("failed to create pusher: %v", err)
	}
	return p
}

func dirEntry(name string) archiveEntry {
	return archiveEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}}
}

func fileEntry(name, body string) archiveEntry {
	return archiveEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, body: body}
}

func linkEntry(name, target string) archiveEntry {
	return archiveEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777}}
}

// TestExtractArchive_Symlinks confirms safe relative links are restored and every escaping shape is skipped without failing the rest.
func TestExtractArchive_Symlinks(t *testing.T) {
	archivePath := buildArchive(t, []archiveEntry{
		dirEntry("mydir"),
		// Links come first to prove files are never written through them.
		linkEntry("mydir/to-file", "a.txt"),
		linkEntry("mydir/sub/to-parent-file", "../a.txt"),
		linkEntry("mydir/to-sub", "sub"),
		linkEntry("mydir/absolute", "/etc/passwd"),
		linkEntry("mydir/escape", "../outside"),
		linkEntry("mydir/self", "."),
		linkEntry("mydir/self/nested-escape", "../outside"),
		dirEntry("mydir/sub"),
		fileEntry("mydir/a.txt", "hello"),
	})

	dir := t.TempDir()
	if err := extractArchive(context.Background(), archivePath, dir); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	restored := map[string]string{
		"to-file":            "a.txt",
		"sub/to-parent-file": "../a.txt",
		"to-sub":             "sub",
		"self":               ".",
	}
	for name, want := range restored {
		got, err := os.Readlink(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("expected symlink %s to be restored: %v", name, err)
			continue
		}
		if got != filepath.FromSlash(want) {
			t.Errorf("symlink %s -> %q, want %q", name, got, want)
		}
	}

	for _, name := range []string{"absolute", "escape"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected unsafe symlink %s to be skipped", name)
		}
	}
	// self/nested-escape would really land in dir's parent, since self points at dir.
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "outside")); !os.IsNotExist(err) {
		t.Error("a symlink nested under another symlink escaped the extraction root")
	}
	if _, err := os.Lstat(filepath.Join(dir, "nested-escape")); !os.IsNotExist(err) {
		t.Error("expected symlink nested under a symlink to be skipped")
	}

	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(got) != "hello" {
		t.Errorf("a.txt = %q (err %v), want %q", got, err, "hello")
	}
}

// TestExtractArchive_PreservesModesAndTimes confirms file and directory permissions and mtimes survive the round trip, regardless of umask.
func TestExtractArchive_PreservesModesAndTimes(t *testing.T) {
	mtime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	exec := fileEntry("mydir/bin/run.sh", "#!/bin/sh\n")
	exec.hdr.Mode = 0o755
	exec.hdr.ModTime = mtime
	private := fileEntry("mydir/secret", "shh")
	private.hdr.Mode = 0o600
	readOnlyDir := dirEntry("mydir/ro")
	readOnlyDir.hdr.Mode = 0o555
	readOnlyDir.hdr.ModTime = mtime

	archivePath := buildArchive(t, []archiveEntry{
		dirEntry("mydir"),
		dirEntry("mydir/bin"),
		exec,
		private,
		readOnlyDir,
		fileEntry("mydir/ro/inside.txt", "x"),
	})

	dir := t.TempDir()
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "ro"), 0o755) })
	if err := extractArchive(context.Background(), archivePath, dir); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	checks := []struct {
		name  string
		mode  os.FileMode
		mtime time.Time
	}{
		{name: "bin/run.sh", mode: 0o755, mtime: mtime},
		{name: "secret", mode: 0o600},
		{name: "ro", mode: 0o555 | os.ModeDir, mtime: mtime},
	}
	for _, c := range checks {
		fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(c.name)))
		if err != nil {
			t.Fatalf("failed to stat %s: %v", c.name, err)
		}
		if got := fi.Mode() & (os.ModePerm | os.ModeDir); got != c.mode {
			t.Errorf("%s mode = %v, want %v", c.name, got, c.mode)
		}
		if !c.mtime.IsZero() && !fi.ModTime().Equal(c.mtime) {
			t.Errorf("%s mtime = %v, want %v", c.name, fi.ModTime(), c.mtime)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ro", "inside.txt")); err != nil {
		t.Errorf("expected file inside read-only dir to be extracted: %v", err)
	}
}

// TestExtractArchive_RejectsPathTraversal is a Zip Slip regression test: an entry name escaping dir via "../" must be rejected, not written outside dir.
func TestExtractArchive_RejectsPathTraversal(t *testing.T) {
	outsideDir := t.TempDir()
	dir := filepath.Join(outsideDir, "extract-root")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create extraction root: %v", err)
	}

	// Once entryTarget drops the "mydir/" prefix, this entry resolves one level above dir, into outsideDir.
	files := map[string]string{"../malicious.txt": "traversal payload"}
	archivePath := archiveFile(t, newArchiveFixture(t, "mydir", files))

	if err := extractArchive(context.Background(), archivePath, dir); err == nil {
		t.Fatal("expected an error for a path-traversal entry, got nil")
	}

	if _, err := os.Stat(filepath.Join(outsideDir, "malicious.txt")); !os.IsNotExist(err) {
		t.Fatal("archive entry escaped the extraction root and was written outside it")
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
	p := newTestPusher(t, root)

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
	p := newTestPusher(t, root)

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

// TestPush_DigestMismatchLeavesDestinationUntouched confirms content that fails verification never replaces an existing directory.
func TestPush_DigestMismatchLeavesDestinationUntouched(t *testing.T) {
	archiveBytes := newArchiveFixture(t, "mydir", map[string]string{"a.txt": "hello"})

	root := t.TempDir()
	existing := filepath.Join(root, "mydir")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("failed to seed existing directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existing, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("failed to seed existing file: %v", err)
	}

	p := newTestPusher(t, root)

	desc := ocispec.Descriptor{
		MediaType: consts.FileLayerMediaType,
		Digest:    digest.FromString("something else entirely"),
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
	if err := w.Close(); err == nil {
		t.Fatal("expected a digest mismatch error from Close, got nil")
	}

	if got, err := os.ReadFile(filepath.Join(existing, "keep.txt")); err != nil || string(got) != "keep" {
		t.Errorf("existing directory was modified: keep.txt = %q, err %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(existing, "a.txt")); !os.IsNotExist(err) {
		t.Error("unverified archive content was extracted")
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, ".mydir.hauler-extract-*"))
	if len(leftovers) != 0 {
		t.Errorf("staging directories left behind: %v", leftovers)
	}
}

// TestPush_RejectsTitleResolvingToDestination guards against a title like "." replacing the whole extraction directory.
func TestPush_RejectsTitleResolvingToDestination(t *testing.T) {
	for _, title := range []string{".", "sub/..", "./"} {
		t.Run(title, func(t *testing.T) {
			root := t.TempDir()
			sentinel := filepath.Join(root, "IMPORTANT.txt")
			if err := os.WriteFile(sentinel, []byte("precious"), 0o644); err != nil {
				t.Fatalf("failed to seed sentinel: %v", err)
			}

			p := newTestPusher(t, root)

			desc := ocispec.Descriptor{
				MediaType: consts.FileLayerMediaType,
				Digest:    digest.FromString("x"),
				Size:      1,
				Annotations: map[string]string{
					ocispec.AnnotationTitle:  title,
					content.AnnotationUnpack: "true",
				},
			}
			if _, err := p.Push(context.Background(), desc); err == nil {
				t.Fatalf("expected Push to reject title %q", title)
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatalf("destination contents were removed: %v", err)
			}
		})
	}
}

// layerBytes stores src through the real directory getter in format and returns the compressed layer bytes.
func layerBytes(t *testing.T, src, format string) []byte {
	t.Helper()
	layer, err := getter.NewClient(getter.ClientOptions{ArchiveFormat: format}).LayerFrom(context.Background(), src)
	if err != nil {
		t.Fatalf("LayerFrom: %v", err)
	}
	rc, err := layer.Compressed()
	if err != nil {
		t.Fatalf("Compressed: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading layer: %v", err)
	}
	return data
}

// TestExtractArchive_DetectsFormatFromContent covers new tar.zst directory layers and older tar.gz ones, with no hint either way.
func TestExtractArchive_DetectsFormatFromContent(t *testing.T) {
	src := filepath.Join(t.TempDir(), "mydir")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, format := range []string{"tar.zst", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "layer")
			if err := os.WriteFile(archivePath, layerBytes(t, src, format), 0o644); err != nil {
				t.Fatal(err)
			}

			dir := t.TempDir()
			if err := extractArchive(context.Background(), archivePath, dir); err != nil {
				t.Fatalf("extractArchive: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "sub", "a.txt"))
			if err != nil || string(got) != "hello" {
				t.Errorf("sub/a.txt = %q (err %v), want %q", got, err, "hello")
			}
		})
	}
}

// TestDirectoryLayersDefaultToZstd confirms the getter default matches `store save` by checking the zstd frame magic.
func TestDirectoryLayersDefaultToZstd(t *testing.T) {
	data := layerBytes(t, t.TempDir(), "")
	if !bytes.HasPrefix(data, []byte{0x28, 0xb5, 0x2f, 0xfd}) {
		t.Errorf("default directory layer is not zstd, starts with % x", data[:4])
	}
}
