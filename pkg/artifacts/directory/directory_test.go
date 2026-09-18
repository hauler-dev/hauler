package directory

import (
	"os"
	"path/filepath"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

func newDirFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}
	return dir
}

func TestNewDirectory_RejectsNonDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write fixture file: %v", err)
	}

	if _, err := NewDirectory(path); err == nil {
		t.Fatal("expected an error for a plain file, got nil")
	}
}

func TestNewDirectory_RejectsMissingPath(t *testing.T) {
	if _, err := NewDirectory(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing path, got nil")
	}
}

func TestDirectory_Manifest(t *testing.T) {
	d, err := NewDirectory(newDirFixture(t))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	m, err := d.Manifest()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if m.Config.MediaType != consts.FileDirectoryConfigMediaType {
		t.Errorf("Config.MediaType = %q, want %q", m.Config.MediaType, consts.FileDirectoryConfigMediaType)
	}
	if len(m.Layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(m.Layers))
	}
}

func TestDirectory_Name(t *testing.T) {
	dir := newDirFixture(t)

	t.Run("derived from path", func(t *testing.T) {
		d, err := NewDirectory(dir)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got, want := d.Name(dir), filepath.Base(dir); got != want {
			t.Errorf("Name() = %q, want %q", got, want)
		}
	})

	t.Run("explicit override wins", func(t *testing.T) {
		d, err := NewDirectory(dir, WithName("custom-name"))
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if got := d.Name(dir); got != "custom-name" {
			t.Errorf("Name() = %q, want %q", got, "custom-name")
		}
	})
}

func TestDirectory_Size(t *testing.T) {
	d, err := NewDirectory(newDirFixture(t))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	size, err := d.Size()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if size <= 0 {
		t.Errorf("Size() = %d, want > 0", size)
	}
}
