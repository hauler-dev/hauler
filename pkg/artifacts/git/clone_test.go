package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "https URL", path: "https://github.com/example/myrepo.git", want: true},
		{name: "http URL", path: "http://internal.example.com/myrepo.git", want: true},
		{name: "ssh URL", path: "ssh://git@github.com/example/myrepo.git", want: true},
		{name: "scp-like shorthand", path: "git@github.com:example/myrepo.git", want: true},
		{name: "local absolute path", path: "/home/user/myrepo.git", want: false},
		{name: "local relative path", path: "myrepo.git", want: false},
		{name: "local path containing an at-sign but no colon after it", path: "/home/user@work/myrepo.git", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsGitURL(tt.path); got != tt.want {
				t.Errorf("IsGitURL(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsNonBareRepo(t *testing.T) {
	t.Run("non-bare working copy with a .git directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("failed to create .git dir: %v", err)
		}
		if !IsNonBareRepo(dir) {
			t.Error("expected true for a directory with a .git subdirectory")
		}
	})

	t.Run("bare repo has no .git directory", func(t *testing.T) {
		if IsNonBareRepo(newBareRepoFixture(t)) {
			t.Error("expected false for an already-bare repo")
		}
	})

	t.Run("plain directory with no .git", func(t *testing.T) {
		if IsNonBareRepo(t.TempDir()) {
			t.Error("expected false for a directory with no .git entry")
		}
	})

	t.Run("URLs are never treated as a local non-bare repo", func(t *testing.T) {
		if IsNonBareRepo("https://github.com/example/myrepo.git") {
			t.Error("expected false for a URL")
		}
	})
}

func TestDeriveGitName(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "https URL", url: "https://github.com/ranchergovernment/harvester-appliance.git", want: "harvester-appliance"},
		{name: "https URL without .git suffix", url: "https://github.com/ranchergovernment/harvester-appliance", want: "harvester-appliance"},
		{name: "scp-like shorthand", url: "git@github.com:ranchergovernment/harvester-appliance.git", want: "harvester-appliance"},
		{name: "ssh URL", url: "ssh://git@github.com/ranchergovernment/harvester-appliance.git", want: "harvester-appliance"},
		{name: "trailing slash", url: "https://github.com/example/myrepo/", want: "myrepo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveGitName(tt.url); got != tt.want {
				t.Errorf("deriveGitName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
