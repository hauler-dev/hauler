package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	// Register the filesystem storage driver for the distribution registry.
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

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

// TestNewFile_BasicAuthRequired verifies --basic-auth actually gates file access end-to-end.
func TestNewFile_BasicAuthRequired(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	ctx := context.Background()
	opts := flags.ServeFilesOpts{
		RootDir:   dir,
		BasicAuth: writeHtpasswdFile(t, "testuser", "testpass"),
	}

	srv, err := NewFile(ctx, opts)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	httpSrv, ok := srv.(*http.Server)
	if !ok {
		t.Fatalf("expected *http.Server, got %T", srv)
	}

	rec := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sample.txt", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without credentials, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sample.txt", nil)
	req.SetBasicAuth("testuser", "testpass")
	httpSrv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid credentials, got %d", rec.Code)
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("got body %q, want %q", rec.Body.String(), "hello world")
	}
}

// TestNewFile_NoBasicAuthByDefault verifies no credentials are required when --basic-auth isn't set.
func TestNewFile_NoBasicAuthByDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	ctx := context.Background()
	opts := flags.ServeFilesOpts{RootDir: dir}

	srv, err := NewFile(ctx, opts)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	httpSrv, ok := srv.(*http.Server)
	if !ok {
		t.Fatalf("expected *http.Server, got %T", srv)
	}

	rec := httptest.NewRecorder()
	httpSrv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sample.txt", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no --basic-auth configured, got %d", rec.Code)
	}
}

// This test guards against a past bug. NewTempRegistry builds its
// Configuration struct by hand, so it must set Catalog.MaxEntries itself.
// Without that line, MaxEntries stays 0. That makes GET /v2/_catalog always
// return an empty list, no matter how much content the test pushes. It also
// makes the registry reject any explicit page size (?n=) with
// PAGINATION_NUMBER_INVALID, because the requested size is always larger
// than the zero-value limit.
func TestNewTempRegistry_CatalogListsPushedRepositories(t *testing.T) {
	ctx := context.Background()
	srv := NewTempRegistry(ctx, t.TempDir())

	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start temp registry: %v", err)
	}
	t.Cleanup(srv.Stop)

	repo := "library/regression"
	ref, err := name.NewTag(srv.Registry()+"/"+repo+":latest", name.WithDefaultRegistry(""))
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}

	img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}

	if err := remote.Write(ref, img); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}

	for _, path := range []string{"/v2/_catalog", "/v2/_catalog?n=100"} {
		resp, err := http.Get("http://" + srv.Registry() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("expected 200 from %s, got %d: %s", path, resp.StatusCode, body)
		}

		var out struct {
			Repositories []string `json:"repositories"`
		}
		err = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("decode catalog response from %s: %v", path, err)
		}

		found := false
		for _, r := range out.Repositories {
			if r == repo {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in catalog repositories from %s, got %v", repo, path, out.Repositories)
		}
	}
}
