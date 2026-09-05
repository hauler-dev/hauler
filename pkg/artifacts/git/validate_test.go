package git

import (
	"os"
	"path/filepath"
	"testing"
)

// newBareRepoFixture builds the minimal on-disk layout of a valid bare git repo and returns its path.
func newBareRepoFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
		t.Fatalf("failed to write HEAD: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
		t.Fatalf("failed to create objects dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs", "heads"), 0o755); err != nil {
		t.Fatalf("failed to create refs dir: %v", err)
	}
	sha := "0000000000000000000000000000000000000000"
	if err := os.WriteFile(filepath.Join(dir, "refs", "heads", "master"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write ref: %v", err)
	}

	return dir
}

func TestValidateBareRepo(t *testing.T) {
	t.Run("valid repo with refs directory", func(t *testing.T) {
		if err := ValidateBareRepo(newBareRepoFixture(t)); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("valid repo with only packed-refs", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
			t.Fatalf("failed to write HEAD: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
			t.Fatalf("failed to create objects dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "packed-refs"), []byte("# pack-refs with: peeled fully-peeled sorted\n"), 0o644); err != nil {
			t.Fatalf("failed to write packed-refs: %v", err)
		}

		if err := ValidateBareRepo(dir); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("missing HEAD file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
			t.Fatalf("failed to create objects dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
			t.Fatalf("failed to create refs dir: %v", err)
		}

		if err := ValidateBareRepo(dir); err == nil {
			t.Fatal("expected an error for a missing HEAD file, got nil")
		}
	})

	t.Run("missing objects directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
			t.Fatalf("failed to write HEAD: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
			t.Fatalf("failed to create refs dir: %v", err)
		}

		if err := ValidateBareRepo(dir); err == nil {
			t.Fatal("expected an error for a missing objects directory, got nil")
		}
	})

	t.Run("missing refs and packed-refs", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
			t.Fatalf("failed to write HEAD: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
			t.Fatalf("failed to create objects dir: %v", err)
		}

		if err := ValidateBareRepo(dir); err == nil {
			t.Fatal("expected an error for missing refs and packed-refs, got nil")
		}
	})

	t.Run("plain non-repo directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("failed to write notes.txt: %v", err)
		}

		if err := ValidateBareRepo(dir); err == nil {
			t.Fatal("expected an error for a plain non-repo directory, got nil")
		}
	})
}
