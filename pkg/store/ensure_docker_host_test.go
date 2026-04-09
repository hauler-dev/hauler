package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDockerHost(t *testing.T) {
	t.Run("DOCKER_HOST already set", func(t *testing.T) {
		t.Setenv("DOCKER_HOST", "unix:///some/custom/docker.sock")

		err := ensureDockerHost()
		if err != nil {
			t.Errorf("ensureDockerHost() returned unexpected error: %v", err)
		}
		if got := os.Getenv("DOCKER_HOST"); got != "unix:///some/custom/docker.sock" {
			t.Errorf("DOCKER_HOST was modified: got %q, want %q", got, "unix:///some/custom/docker.sock")
		}
	})

	t.Run("no socket found returns error", func(t *testing.T) {
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			t.Skip("default docker socket exists at /var/run/docker.sock")
		}

		tmp := t.TempDir()
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("HOME", tmp)

		err := ensureDockerHost()
		if err == nil {
			t.Fatal("ensureDockerHost() expected error when no socket exists, got nil")
		}
		if !strings.Contains(err.Error(), "no Docker socket found") {
			t.Errorf("error %q does not contain %q", err.Error(), "no Docker socket found")
		}
	})

	t.Run("sets DOCKER_HOST for Docker Desktop socket", func(t *testing.T) {
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			t.Skip("default docker socket exists at /var/run/docker.sock")
		}

		tmp := t.TempDir()
		t.Setenv("DOCKER_HOST", "")
		t.Setenv("HOME", tmp)

		sockDir := filepath.Join(tmp, ".docker", "run")
		if err := os.MkdirAll(sockDir, 0o755); err != nil {
			t.Fatalf("creating socket dir: %v", err)
		}
		sockPath := filepath.Join(sockDir, "docker.sock")
		f, err := os.Create(sockPath)
		if err != nil {
			t.Fatalf("creating socket file: %v", err)
		}
		f.Close()

		if err := ensureDockerHost(); err != nil {
			t.Fatalf("ensureDockerHost() unexpected error: %v", err)
		}

		want := "unix://" + sockPath
		if got := os.Getenv("DOCKER_HOST"); got != want {
			t.Errorf("DOCKER_HOST = %q, want %q", got, want)
		}
	})
}
