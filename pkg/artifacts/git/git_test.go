package git

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// newNonBareRepoFixture creates a real, valid (non-bare) git repository with one commit, and returns its path.
func newNonBareRepoFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("failed to get worktree: %v", err)
	}

	sig := &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}
	if _, err := wt.Commit("initial commit", &gogit.CommitOptions{AllowEmptyCommits: true, Author: sig}); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	return dir
}

func TestGit_ComputeLocalRepo(t *testing.T) {
	dir := newBareRepoFixture(t)
	g := NewGit(dir)

	m, err := g.Manifest()
	if err != nil {
		t.Fatalf("Manifest failed: %v", err)
	}
	if string(m.Config.MediaType) != consts.GitRepoConfigMediaType {
		t.Errorf("config media type = %q, want %q", m.Config.MediaType, consts.GitRepoConfigMediaType)
	}
	if len(m.Layers) != 1 {
		t.Fatalf("expected exactly 1 layer, got %d", len(m.Layers))
	}

	raw, err := g.RawConfig()
	if err != nil {
		t.Fatalf("RawConfig failed: %v", err)
	}
	var cfg gitConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	if cfg.Reference != dir {
		t.Errorf("config reference = %q, want %q", cfg.Reference, dir)
	}

	layers, err := g.Layers()
	if err != nil {
		t.Fatalf("Layers failed: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected exactly 1 layer, got %d", len(layers))
	}
}

// TestGit_ComputeMirrorsNonBareRepo verifies compute() auto-detects a normal (non-bare) local working copy and mirrors it into a bare repo before storing, rather than requiring the caller to bare-clone it themselves.
func TestGit_ComputeMirrorsNonBareRepo(t *testing.T) {
	dir := newNonBareRepoFixture(t)
	g := NewGit(dir)

	m, err := g.Manifest()
	if err != nil {
		t.Fatalf("Manifest failed: %v", err)
	}
	if string(m.Config.MediaType) != consts.GitRepoConfigMediaType {
		t.Errorf("config media type = %q, want %q", m.Config.MediaType, consts.GitRepoConfigMediaType)
	}

	raw, err := g.RawConfig()
	if err != nil {
		t.Fatalf("RawConfig failed: %v", err)
	}
	var cfg gitConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("failed to unmarshal config: %v", err)
	}
	if cfg.Reference != dir {
		t.Errorf("config reference = %q, want the original working copy path %q", cfg.Reference, dir)
	}

	if err := g.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestGit_RejectsPlainDirectory(t *testing.T) {
	dir := t.TempDir()
	g := NewGit(dir)

	if _, err := g.Manifest(); err == nil {
		t.Fatal("expected an error for a directory that is not a git repository at all, got nil")
	}
}

func TestGit_Name(t *testing.T) {
	dir := newBareRepoFixture(t)

	t.Run("no override derives from the local path", func(t *testing.T) {
		g := NewGit(dir)
		if got, want := g.Name(dir), filepath.Base(dir); got != want {
			t.Errorf("Name() = %q, want %q", got, want)
		}
	})

	t.Run("WithName takes precedence", func(t *testing.T) {
		g := NewGit(dir, WithName("renamed"))
		if got := g.Name(dir); got != "renamed" {
			t.Errorf("Name() = %q, want %q", got, "renamed")
		}
	})

	t.Run("URL derives via deriveGitName, ignoring the local client", func(t *testing.T) {
		g := NewGit("https://github.com/example/myrepo.git")
		if got, want := g.Name("https://github.com/example/myrepo.git"), "myrepo"; got != want {
			t.Errorf("Name() = %q, want %q", got, want)
		}
	})
}

func TestGit_Size(t *testing.T) {
	dir := newBareRepoFixture(t)
	g := NewGit(dir)

	size, err := g.Size()
	if err != nil {
		t.Fatalf("Size failed: %v", err)
	}
	if size <= 0 {
		t.Errorf("Size() = %d, want > 0", size)
	}
}

func TestGit_Close_NoOpWithoutClone(t *testing.T) {
	dir := newBareRepoFixture(t)
	g := NewGit(dir)

	if err := g.Close(); err != nil {
		t.Fatalf("Close on an uncloned Git should be a no-op, got: %v", err)
	}

	if _, err := g.Manifest(); err != nil {
		t.Fatalf("Manifest should still work after Close on an uncloned Git: %v", err)
	}
}
