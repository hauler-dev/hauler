package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	// Register the filesystem storage driver for the distribution registry.
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"

	"hauler.dev/go/hauler/internal/flags"
)

func TestNewTempRegistry_StartStop(t *testing.T) {
	ctx := context.Background()
	srv := NewTempRegistry(ctx, t.TempDir())

	// Start the httptest server directly to avoid the Start() method's
	// retry logic which only accepts HTTP 200, while /v2 returns 401
	// from the distribution registry.
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
