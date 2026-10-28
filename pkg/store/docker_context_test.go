package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// A selected context must determine the client endpoint, even when a default
// daemon socket is also available. The endpoint need not exist to resolve it.
func TestLocalDockerClientUsesSelectedContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	writeDockerContext(t, dir, "rootless", "unix:///selected/rootless/docker.sock")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"rootless"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dc, err := newSpoolingDaemonClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	got := dc.Client.(interface{ DaemonHost() string }).DaemonHost()
	if got != "unix:///selected/rootless/docker.sock" {
		t.Fatalf("Docker endpoint = %q, want selected rootless context", got)
	}
	if got := os.Getenv("DOCKER_HOST"); got != "" {
		t.Fatalf("client creation changed DOCKER_HOST to %q", got)
	}
}

func writeDockerContext(t *testing.T, dir, name, host string) {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	metaDir := filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"Name":"` + name + `","Endpoints":{"docker":{"Host":"` + host + `","SkipTLSVerify":false}}}`
	if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocalDockerClientExplicitSelection(t *testing.T) {
	for _, tc := range []struct {
		name, host, context, want string
	}{
		{name: "context environment overrides stored selection", context: "other", want: "unix:///selected/other/docker.sock"},
		{name: "explicit host overrides stored selection", host: "unix:///explicit/docker.sock", want: "unix:///explicit/docker.sock"},
		{name: "explicit host overrides context environment", host: "unix:///explicit/docker.sock", context: "other", want: "unix:///explicit/docker.sock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("DOCKER_CONFIG", dir)
			t.Setenv("DOCKER_HOST", tc.host)
			t.Setenv("DOCKER_CONTEXT", tc.context)
			writeDockerContext(t, dir, "rootless", "unix:///selected/rootless/docker.sock")
			writeDockerContext(t, dir, "other", "unix:///selected/other/docker.sock")
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"rootless"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			dc, err := newSpoolingDaemonClient(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer dc.Close()
			if got := dc.Client.(interface{ DaemonHost() string }).DaemonHost(); got != tc.want {
				t.Fatalf("Docker endpoint = %q, want %q", got, tc.want)
			}
			if got := os.Getenv("DOCKER_HOST"); got != tc.host {
				t.Fatalf("client creation changed DOCKER_HOST from %q to %q", tc.host, got)
			}
		})
	}
}

func TestLocalDockerClientRejectsInvalidContext(t *testing.T) {
	for _, tc := range []struct{ name, config, context string }{
		{name: "missing stored context", config: `{"currentContext":"missing"}`},
		{name: "missing environment context", config: `{}`, context: "missing"},
		{name: "invalid client configuration", config: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("DOCKER_CONFIG", dir)
			t.Setenv("DOCKER_HOST", "")
			t.Setenv("DOCKER_CONTEXT", tc.context)
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			dc, err := newSpoolingDaemonClient(t.TempDir())
			if dc != nil {
				defer dc.Close()
			}
			if err == nil {
				t.Fatal("expected invalid Docker context/configuration to fail, not use another daemon")
			}
		})
	}
}

func TestLocalDockerClientRejectsEmptyContextEndpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "empty")
	writeDockerContext(t, dir, "empty", "")
	dc, err := newSpoolingDaemonClient(t.TempDir())
	if dc != nil {
		defer dc.Close()
	}
	if err == nil {
		t.Fatal("expected empty selected endpoint to fail, not use the default daemon")
	}
}

func TestLocalDockerClientUsesContextTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("API-Version", "1.55")
		w.Write([]byte("OK"))
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "secure")
	// Named contexts use their own TLS settings, not the default host's files.
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", filepath.Join(dir, "nonexistent-default-certs"))
	writeDockerContext(t, dir, "secure", "tcp://"+strings.TrimPrefix(server.URL, "https://"))
	sum := sha256.Sum256([]byte("secure"))
	tlsDir := filepath.Join(dir, "contexts", "tls", hex.EncodeToString(sum[:]), "docker")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(tlsDir, "ca.pem"), ca, 0o600); err != nil {
		t.Fatal(err)
	}
	dc, err := newSpoolingDaemonClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := dc.Client.(*client.Client).Ping(ctx, client.PingOptions{}); err != nil {
		t.Fatalf("ping selected TLS context: %v", err)
	}
	// Invalid context CA material must never become an unverified connection.
	if err := os.WriteFile(filepath.Join(tlsDir, "ca.pem"), []byte("invalid CA"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid, err := newSpoolingDaemonClient(t.TempDir())
	if invalid != nil {
		defer invalid.Close()
	}
	if err == nil {
		t.Fatal("expected invalid context CA to fail")
	}
}
