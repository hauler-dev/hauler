package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	// Register the filesystem storage driver for the distribution registry.
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"

	"hauler.dev/go/hauler/v2/internal/flags"
)

func TestNewTempRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	srv := NewTempRegistry(ctx, t.TempDir())

	// start the httptest server directly to avoid the retry logic which only accepts HTTP 200
	// while /v2 returns 401 from the distribution registry.
	srv.Server.Start()
	t.Cleanup(func() { srv.Stop() })

	resp, err := http.Get(srv.Server.URL + "/v2")
	if err != nil {
		t.Fatalf("expected GET /v2 to succeed, got error: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 200 or 401, got %d", resp.StatusCode)
	}

	// Stop and verify unreachable.
	srv.Stop()

	_, err = http.Get(srv.Server.URL + "/v2")
	if err == nil {
		t.Fatal("expected error after stopping server, got nil")
	}
}

func TestNewTempRegistry_Registry(t *testing.T) {
	ctx := context.Background()
	srv := NewTempRegistry(ctx, t.TempDir())

	srv.Server.Start()
	t.Cleanup(func() { srv.Stop() })

	host := srv.Registry()
	if host == "" {
		t.Fatal("expected non-empty registry host")
	}
	if strings.Contains(host, "http://") {
		t.Fatalf("registry host should not contain protocol prefix, got %q", host)
	}
}

func TestNewFile_Configuration(t *testing.T) {
	ctx := context.Background()
	opts := flags.ServeFilesOpts{
		RootDir: t.TempDir(),
		Port:    0,
		Timeout: 0,
	}

	srv, err := NewFile(ctx, opts)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

// this is the only test in the package allowed to enable prometheus, since it registers
// on http.DefaultServeMux and a second registration would panic.
func TestConfigureDebugServer_Prometheus(t *testing.T) {
	// grab a free port and release it so ConfigureDebugServer can bind it
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()

	cfg := &configuration.Configuration{}
	cfg.HTTP.Debug.Addr = addr
	cfg.HTTP.Debug.Prometheus.Enabled = true
	cfg.HTTP.Debug.Prometheus.Path = "/metrics"

	ConfigureDebugServer(cfg)

	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = http.Get("http://" + addr + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("expected GET /metrics to eventually succeed, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 from the prometheus handler, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !strings.Contains(string(body), "go_gc_duration_seconds") {
		t.Fatalf("expected prometheus-formatted metrics output, got: %s", body)
	}
}

// an empty Debug.Addr should just no-op, not start a listener.
func TestConfigureDebugServer_NoAddr(t *testing.T) {
	ConfigureDebugServer(&configuration.Configuration{})
}

func TestNewFile_DefaultPort(t *testing.T) {
	ctx := context.Background()
	opts := flags.ServeFilesOpts{
		RootDir: t.TempDir(),
	}

	srv, err := NewFile(ctx, opts)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}
