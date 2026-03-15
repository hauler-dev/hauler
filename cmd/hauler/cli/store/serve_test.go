package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
)

// writeIndexJSON writes a minimal valid OCI index.json to dir so that
// validateStoreExists can find it. NewLayout only writes index.json on
// SaveIndex, which is triggered by adding content — so tests that need a
// "valid store on disk" must create the file themselves.
func writeIndexJSON(t *testing.T, dir string) {
	t.Helper()
	const minimal = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(minimal), 0o644); err != nil {
		t.Fatalf("writeIndexJSON: %v", err)
	}
}

func TestValidateStoreExists(t *testing.T) {
	t.Run("valid store", func(t *testing.T) {
		s := newTestStore(t)
		writeIndexJSON(t, s.Root)
		if err := validateStoreExists(s); err != nil {
			t.Errorf("validateStoreExists on valid store: %v", err)
		}
	})

	t.Run("missing index.json", func(t *testing.T) {
		s := newTestStore(t)
		err := validateStoreExists(s)
		if err == nil {
			t.Fatal("expected error for missing index.json, got nil")
		}
		if !strings.Contains(err.Error(), "no store found") {
			t.Errorf("expected 'no store found' in error, got: %v", err)
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		s := newTestStore(t)
		// Point the layout root at a path that does not exist.
		s.Root = filepath.Join(t.TempDir(), "does-not-exist", "nested")
		err := validateStoreExists(s)
		if err == nil {
			t.Fatal("expected error for nonexistent dir, got nil")
		}
	})
}

func TestDefaultRegistryConfig(t *testing.T) {
	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{
		Port:    consts.DefaultRegistryPort,
		RootDir: rootDir,
	}
	rso := defaultRootOpts(rootDir)
	ro := defaultCliOpts()

	cfg := DefaultRegistryConfig(o, rso, ro)
	if cfg == nil {
		t.Fatal("DefaultRegistryConfig returned nil")
	}

	// Port
	wantAddr := ":5000"
	if cfg.HTTP.Addr != wantAddr {
		t.Errorf("HTTP.Addr = %q, want %q", cfg.HTTP.Addr, wantAddr)
	}

	// No TLS by default.
	if cfg.HTTP.TLS.Certificate != "" || cfg.HTTP.TLS.Key != "" {
		t.Errorf("expected no TLS cert/key by default, got cert=%q key=%q",
			cfg.HTTP.TLS.Certificate, cfg.HTTP.TLS.Key)
	}

	// Log level matches ro.LogLevel.
	if string(cfg.Log.Level) != ro.LogLevel {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, ro.LogLevel)
	}

	// Storage rootdirectory.
	fsParams := cfg.Storage["filesystem"]
	if fsParams == nil {
		t.Fatal("storage.filesystem not set")
	}
	if fsParams["rootdirectory"] != rootDir {
		t.Errorf("storage.filesystem.rootdirectory = %v, want %q", fsParams["rootdirectory"], rootDir)
	}

	// URL allow rules.
	if len(cfg.Validation.Manifests.URLs.Allow) == 0 {
		t.Error("Validation.Manifests.URLs.Allow is empty, want at least one rule")
	}
}

func TestDefaultRegistryConfig_WithTLS(t *testing.T) {
	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{
		Port:    consts.DefaultRegistryPort,
		RootDir: rootDir,
		TLSCert: "/path/to/cert.pem",
		TLSKey:  "/path/to/key.pem",
	}
	rso := defaultRootOpts(rootDir)
	ro := defaultCliOpts()

	cfg := DefaultRegistryConfig(o, rso, ro)
	if cfg.HTTP.TLS.Certificate != o.TLSCert {
		t.Errorf("TLS.Certificate = %q, want %q", cfg.HTTP.TLS.Certificate, o.TLSCert)
	}
	if cfg.HTTP.TLS.Key != o.TLSKey {
		t.Errorf("TLS.Key = %q, want %q", cfg.HTTP.TLS.Key, o.TLSKey)
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	// Write a minimal valid distribution registry config.
	cfg := `
version: 0.1
log:
  level: info
storage:
  filesystem:
    rootdirectory: /tmp/registry
  cache:
    blobdescriptor: inmemory
http:
  addr: :5000
  headers:
    X-Content-Type-Options: [nosniff]
`
	f, err := os.CreateTemp(t.TempDir(), "registry-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	f.Close()

	got, err := loadConfig(f.Name())
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if got == nil {
		t.Fatal("loadConfig returned nil config")
	}
	if got.HTTP.Addr != ":5000" {
		t.Errorf("HTTP.Addr = %q, want %q", got.HTTP.Addr, ":5000")
	}
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/path/to/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent config file, got nil")
	}
}
