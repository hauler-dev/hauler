package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/moby/moby/client"
)

func TestLocalDockerClientDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "default")
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	dc, err := newSpoolingDaemonClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	if got := dc.Client.(interface{ DaemonHost() string }).DaemonHost(); got != client.DefaultDockerHost {
		t.Fatalf("Docker endpoint = %q, want %q", got, client.DefaultDockerHost)
	}
	if got := os.Getenv("DOCKER_HOST"); got != "" {
		t.Fatalf("DOCKER_HOST was modified to %q", got)
	}
}

func TestLocalDockerClientDesktopFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix Docker Desktop fallback")
	}
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		t.Skip("default Docker socket exists")
	}
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("HOME", dir)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")
	sockDir := filepath.Join(dir, ".docker", "run")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(sockDir, "docker.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dc, err := newSpoolingDaemonClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	if got := dc.Client.(interface{ DaemonHost() string }).DaemonHost(); got != "unix://"+sock {
		t.Fatalf("Docker endpoint = %q, want Desktop socket %q", got, sock)
	}
	if got := os.Getenv("DOCKER_HOST"); got != "" {
		t.Fatalf("DOCKER_HOST was modified to %q", got)
	}
}
