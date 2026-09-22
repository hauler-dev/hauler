package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hauler.dev/go/hauler/v2/internal/flags"
)

const (
	testSHAMaster = "6d473dcbdcd310ca4264237f6dcf5392c8b95153"
	testSHATag    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// newBareRepoFixture builds the minimal on-disk layout of a bare git repo, no git binary involved, just the files a dumb HTTP client and updateServerInfo care about.
func newBareRepoFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "HEAD"), "ref: refs/heads/master\n")
	mustMkdirAll(t, filepath.Join(dir, "objects", "pack"))
	mustMkdirAll(t, filepath.Join(dir, "refs", "heads"))
	mustMkdirAll(t, filepath.Join(dir, "refs", "tags"))
	mustWriteFile(t, filepath.Join(dir, "refs", "heads", "master"), testSHAMaster+"\n")
	mustWriteFile(t, filepath.Join(dir, "refs", "tags", "v1.0.0"), testSHATag+"\n")

	return dir
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
}

func TestValidateBareRepo(t *testing.T) {
	t.Run("valid repo", func(t *testing.T) {
		if err := validateBareRepo(newBareRepoFixture(t)); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("missing HEAD", func(t *testing.T) {
		dir := newBareRepoFixture(t)
		if err := os.Remove(filepath.Join(dir, "HEAD")); err != nil {
			t.Fatalf("failed to remove HEAD: %v", err)
		}
		if err := validateBareRepo(dir); err == nil {
			t.Fatal("expected an error for a missing HEAD file, got nil")
		}
	})

	t.Run("missing objects directory", func(t *testing.T) {
		dir := newBareRepoFixture(t)
		if err := os.RemoveAll(filepath.Join(dir, "objects")); err != nil {
			t.Fatalf("failed to remove objects: %v", err)
		}
		if err := validateBareRepo(dir); err == nil {
			t.Fatal("expected an error for a missing objects directory, got nil")
		}
	})

	t.Run("no refs and no packed-refs", func(t *testing.T) {
		dir := newBareRepoFixture(t)
		if err := os.RemoveAll(filepath.Join(dir, "refs")); err != nil {
			t.Fatalf("failed to remove refs: %v", err)
		}
		if err := validateBareRepo(dir); err == nil {
			t.Fatal("expected an error when neither refs nor packed-refs exist, got nil")
		}
	})

	t.Run("packed-refs satisfies the refs requirement", func(t *testing.T) {
		dir := newBareRepoFixture(t)
		if err := os.RemoveAll(filepath.Join(dir, "refs")); err != nil {
			t.Fatalf("failed to remove refs: %v", err)
		}
		mustWriteFile(t, filepath.Join(dir, "packed-refs"), testSHAMaster+" refs/heads/master\n")
		if err := validateBareRepo(dir); err != nil {
			t.Fatalf("expected no error with packed-refs present, got: %v", err)
		}
	})

	t.Run("empty directory argument", func(t *testing.T) {
		if err := validateBareRepo(""); err == nil {
			t.Fatal("expected an error for an empty directory, got nil")
		}
	})
}

func TestCollectRefs_LooseOnly(t *testing.T) {
	dir := newBareRepoFixture(t)

	refs, err := collectRefs(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if refs["refs/heads/master"] != testSHAMaster {
		t.Errorf("refs/heads/master = %q, want %q", refs["refs/heads/master"], testSHAMaster)
	}
	if refs["refs/tags/v1.0.0"] != testSHATag {
		t.Errorf("refs/tags/v1.0.0 = %q, want %q", refs["refs/tags/v1.0.0"], testSHATag)
	}
}

// TestCollectRefs_PackedRefsIgnoresCommentsAndPeeledLines checks the two line shapes real packed-refs files carry beyond "<sha> <name>".
func TestCollectRefs_PackedRefsIgnoresCommentsAndPeeledLines(t *testing.T) {
	dir := newBareRepoFixture(t)
	if err := os.RemoveAll(filepath.Join(dir, "refs")); err != nil {
		t.Fatalf("failed to remove refs: %v", err)
	}
	mustMkdirAll(t, filepath.Join(dir, "refs"))

	packed := "# pack-refs with: peeled fully-peeled sorted\n" +
		testSHATag + " refs/tags/v1.0.0\n" +
		"^cccccccccccccccccccccccccccccccccccccccc\n"
	mustWriteFile(t, filepath.Join(dir, "packed-refs"), packed)

	refs, err := collectRefs(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d: %v", len(refs), refs)
	}
	if refs["refs/tags/v1.0.0"] != testSHATag {
		t.Errorf("refs/tags/v1.0.0 = %q, want %q", refs["refs/tags/v1.0.0"], testSHATag)
	}
}

// TestCollectRefs_LooseOverridesPacked matches real git's ref resolution order.
func TestCollectRefs_LooseOverridesPacked(t *testing.T) {
	dir := newBareRepoFixture(t)

	staleSHA := "1111111111111111111111111111111111111"
	mustWriteFile(t, filepath.Join(dir, "packed-refs"), staleSHA+" refs/heads/master\n")

	refs, err := collectRefs(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if refs["refs/heads/master"] != testSHAMaster {
		t.Errorf("refs/heads/master = %q, want the loose ref %q to win over the packed one", refs["refs/heads/master"], testSHAMaster)
	}
}

func TestUpdateServerInfo(t *testing.T) {
	dir := newBareRepoFixture(t)
	mustWriteFile(t, filepath.Join(dir, "objects", "pack", "pack-abc.pack"), "")
	mustWriteFile(t, filepath.Join(dir, "objects", "pack", "pack-abc.idx"), "")

	if err := updateServerInfo(dir); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	infoRefs, err := os.ReadFile(filepath.Join(dir, "info", "refs"))
	if err != nil {
		t.Fatalf("failed to read generated info/refs: %v", err)
	}
	if !strings.Contains(string(infoRefs), testSHAMaster+"\trefs/heads/master\n") {
		t.Errorf("info/refs missing the master entry, got: %s", infoRefs)
	}

	packs, err := os.ReadFile(filepath.Join(dir, "objects", "info", "packs"))
	if err != nil {
		t.Fatalf("failed to read generated objects/info/packs: %v", err)
	}
	if string(packs) != "P pack-abc.pack\n\n" {
		t.Errorf("objects/info/packs = %q, want %q", packs, "P pack-abc.pack\n\n")
	}
}

func TestNewGit_NoRepos(t *testing.T) {
	ctx := context.Background()
	if _, err := NewGit(ctx, flags.ServeGitOpts{}, map[string]string{}); err == nil {
		t.Fatal("expected an error when no repositories are given, got nil")
	}
}

func TestNewGit_RejectsNonRepoDirectory(t *testing.T) {
	ctx := context.Background()
	repos := map[string]string{"bad": t.TempDir()}

	if _, err := NewGit(ctx, flags.ServeGitOpts{}, repos); err == nil {
		t.Fatal("expected an error for a directory that isn't a bare repo, got nil")
	}
}

// TestNewGit_MultiRepo verifies each repo is served under its own /<name>/ prefix and the root path lists them, the core of the multi-repo design.
func TestNewGit_MultiRepo(t *testing.T) {
	repos := map[string]string{
		"myorg/repo-a": newBareRepoFixture(t),
		"repo-b":       newBareRepoFixture(t),
	}

	ctx := context.Background()
	srv, err := NewGit(ctx, flags.ServeGitOpts{}, repos)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	httpSrv := srv.(*http.Server)

	for name := range repos {
		rec := httptest.NewRecorder()
		httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name+"/info/refs", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET /%s/info/refs = %d, want 200", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "refs/heads/master") {
			t.Errorf("GET /%s/info/refs missing refs/heads/master, got: %s", name, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	for name := range repos {
		if !strings.Contains(rec.Body.String(), name) {
			t.Errorf("root listing missing repo %q, got: %s", name, rec.Body.String())
		}
	}

	rec = httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no-such-repo/info/refs", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /no-such-repo/info/refs = %d, want 404", rec.Code)
	}
}

// TestNewGit_BasicAuthRequired drives NewGit's returned *http.Server.Handler directly, same pattern as TestNewFile_BasicAuthRequired.
func TestNewGit_BasicAuthRequired(t *testing.T) {
	repos := map[string]string{"repo": newBareRepoFixture(t)}

	ctx := context.Background()
	opts := flags.ServeGitOpts{
		BasicAuth: writeHtpasswdFile(t, "gituser", "gitpass"),
	}

	srv, err := NewGit(ctx, opts, repos)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	httpSrv := srv.(*http.Server)

	rec := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/repo/info/refs", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without credentials, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repo/info/refs", nil)
	req.SetBasicAuth("gituser", "gitpass")
	httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid credentials, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "refs/heads/master") {
		t.Errorf("expected served info/refs to contain refs/heads/master, got: %s", rec.Body.String())
	}
}
