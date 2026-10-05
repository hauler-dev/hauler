package store

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/distribution/distribution/v3/registry/handlers"
	goname "github.com/google/go-containerregistry/pkg/name"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/internal/server"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	gitartifact "hauler.dev/go/hauler/v2/pkg/artifacts/git"
	"hauler.dev/go/hauler/v2/pkg/consts"
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
	if cfg.Storage[server.StoreDriverName] != nil {
		t.Errorf("storage.%s set without --in-place", server.StoreDriverName)
	}

	// URL allow rules.
	if len(cfg.Validation.Manifests.URLs.Allow) == 0 {
		t.Error("Validation.Manifests.URLs.Allow is empty, want at least one rule")
	}

	// Catalog/Tags entry caps. configuration.Parse applies these defaults
	// when a config is loaded from YAML, but a hand-built Configuration (as
	// produced here) leaves them at the zero value unless set explicitly.
	// A zero Catalog.MaxEntries makes GET /v2/_catalog always return an
	// empty repository list, regardless of what's actually stored.
	if cfg.Catalog.MaxEntries != consts.DefaultRegistryCatalogMaxEntries {
		t.Errorf("Catalog.MaxEntries = %d, want %d", cfg.Catalog.MaxEntries, consts.DefaultRegistryCatalogMaxEntries)
	}
	if cfg.Tags.MaxTags != consts.DefaultRegistryTagsMaxEntries {
		t.Errorf("Tags.MaxTags = %d, want %d", cfg.Tags.MaxTags, consts.DefaultRegistryTagsMaxEntries)
	}
}

// --in-place swaps the filesystem driver for the in-place store driver.
func TestDefaultRegistryConfig_InPlace(t *testing.T) {
	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{RootDir: rootDir, InPlace: true}
	rso := defaultRootOpts(rootDir)

	cfg := DefaultRegistryConfig(o, rso, defaultCliOpts())
	if cfg.Storage["filesystem"] != nil {
		t.Error("storage.filesystem should not be set with --in-place")
	}
	params := cfg.Storage[server.StoreDriverName]
	if params == nil {
		t.Fatalf("storage.%s not set", server.StoreDriverName)
	}
	if params["rootdirectory"] != rootDir || params["store"] != rso.StoreDir {
		t.Errorf("storage.%s = %v, want rootdirectory %q and store %q", server.StoreDriverName, params, rootDir, rso.StoreDir)
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

func TestDefaultRegistryConfig_NoBasicAuthByDefault(t *testing.T) {
	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{
		Port:    consts.DefaultRegistryPort,
		RootDir: rootDir,
	}
	rso := defaultRootOpts(rootDir)
	ro := defaultCliOpts()

	cfg := DefaultRegistryConfig(o, rso, ro)
	if cfg.Auth != nil {
		t.Errorf("Auth = %v, want nil when --basic-auth is not set", cfg.Auth)
	}
}

// TestDefaultRegistryConfig_WithBasicAuth verifies --basic-auth produces an auth.htpasswd config block.
func TestDefaultRegistryConfig_WithBasicAuth(t *testing.T) {
	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{
		Port:           consts.DefaultRegistryPort,
		RootDir:        rootDir,
		BasicAuth:      "/opt/hauler/registry-htpasswd",
		BasicAuthRealm: "hauler-registry",
	}
	rso := defaultRootOpts(rootDir)
	ro := defaultCliOpts()

	cfg := DefaultRegistryConfig(o, rso, ro)

	htpasswd := cfg.Auth["htpasswd"]
	if htpasswd == nil {
		t.Fatal("Auth[\"htpasswd\"] not set")
	}
	if htpasswd["path"] != o.BasicAuth {
		t.Errorf("Auth[\"htpasswd\"][\"path\"] = %v, want %q", htpasswd["path"], o.BasicAuth)
	}
	if htpasswd["realm"] != o.BasicAuthRealm {
		t.Errorf("Auth[\"htpasswd\"][\"realm\"] = %v, want %q", htpasswd["realm"], o.BasicAuthRealm)
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

// TestExtractGitRepos_SkipsUnsafeName is a regression test: a stored repo named ".." used to make serve git RemoveAll the parent of --directory.
func TestExtractGitRepos_SkipsUnsafeName(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	repoDir := newBareGitRepoFixture(t, "myrepo.git")

	// Bypass AddGitCmd's own name check, the way a haul built elsewhere could.
	for _, ref := range []string{"hauler/..:latest", "hauler/good:latest"} {
		if _, err := s.AddArtifact(ctx, gitartifact.NewGit(repoDir, gitartifact.WithContext(ctx)), ref); err != nil {
			t.Fatalf("AddArtifact %s: %v", ref, err)
		}
	}

	parent := t.TempDir()
	sentinel := filepath.Join(parent, "SENTINEL.txt")
	if err := os.WriteFile(sentinel, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}

	repos, err := extractGitRepos(ctx, s, filepath.Join(parent, "gitroot"), defaultCliOpts())
	if err != nil {
		t.Fatalf("extractGitRepos: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("parent of --directory was modified: %v", err)
	}
	if _, ok := repos[".."]; ok {
		t.Error("expected the unsafe repo to be skipped")
	}
	if _, ok := repos["good"]; !ok {
		t.Errorf("expected the valid repo to still be served, got %v", repos)
	}
}

// TestServeRegistry_InPlace serves a store through the hauler storage driver (no copy) and
// pulls back an image, its cosign sig, a multi-arch index and a file, then checks nothing
// was written to the registry directory.
func TestServeRegistry_InPlace(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	srcImg := seedImage(t, srcHost, "test/signed", "v1")
	seedCosignV2Artifacts(t, srcHost, "test/signed", srcImg)
	seedIndex(t, srcHost, "test/multi", "v2")
	fileURL := seedFileInHTTPServer(t, "data.txt", "hello from the store")

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if _, err := s.AddImage(ctx, srcHost+"/test/signed:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/test/multi:v2"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}
	if err := storeFile(ctx, s, v1.File{Path: fileURL}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{RootDir: rootDir, ReadOnly: true, InPlace: true}
	srv := httptest.NewServer(handlers.NewApp(ctx, DefaultRegistryConfig(o, rso, ro)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	opts := []remote.Option{remote.WithTransport(srv.Client().Transport)}

	pullImage := func(ref string) gcrv1.Image {
		t.Helper()
		r, err := goname.ParseReference(host+"/"+ref, goname.Insecure)
		if err != nil {
			t.Fatal(err)
		}
		img, err := remote.Image(r, opts...)
		if err != nil {
			t.Fatalf("pull %s: %v", ref, err)
		}
		layers, err := img.Layers()
		if err != nil {
			t.Fatalf("layers %s: %v", ref, err)
		}
		for _, l := range layers {
			rc, err := l.Compressed() // remote verifies the digest on read
			if err != nil {
				t.Fatalf("layer %s: %v", ref, err)
			}
			if _, err := io.Copy(io.Discard, rc); err != nil {
				t.Fatalf("read layer %s: %v", ref, err)
			}
			rc.Close()
		}
		return img
	}

	pullImage("test/signed:v1")
	hash, err := srcImg.Digest()
	if err != nil {
		t.Fatal(err)
	}
	pullImage("test/signed:" + strings.ReplaceAll(hash.String(), ":", "-") + ".sig")

	idxRef, _ := goname.ParseReference(host+"/test/multi:v2", goname.Insecure)
	idx, err := remote.Index(idxRef, opts...)
	if err != nil {
		t.Fatalf("pull index: %v", err)
	}
	im, err := idx.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range im.Manifests {
		pullImage("test/multi@" + m.Digest.String())
	}

	fileImg := pullImage(consts.DefaultNamespace + "/data.txt:latest")
	layers, _ := fileImg.Layers()
	rc, err := layers[0].Compressed()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "hello from the store" {
		t.Errorf("file content = %q", got)
	}

	reg, _ := goname.NewRegistry(host, goname.Insecure)
	repos, err := remote.Catalog(ctx, reg, opts...)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	for _, want := range []string{"test/signed", "test/multi", consts.DefaultNamespace + "/data.txt"} {
		if !slices.Contains(repos, want) {
			t.Errorf("catalog %v missing %s", repos, want)
		}
	}

	if entries, _ := os.ReadDir(rootDir); len(entries) != 0 {
		t.Errorf("registry directory should stay empty, has %d entries", len(entries))
	}
}

// TestServeRegistry_InPlace_Writable checks --readonly=false: new pushes land in the registry
// directory, and re-pushing content the store already serves succeeds.
func TestServeRegistry_InPlace_Writable(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	stored := seedImage(t, srcHost, "test/stored", "v1")
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/test/stored:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	rootDir := t.TempDir()
	o := &flags.ServeRegistryOpts{RootDir: rootDir, ReadOnly: false, InPlace: true}
	srv := httptest.NewServer(handlers.NewApp(ctx, DefaultRegistryConfig(o, rso, ro)))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	opts := []remote.Option{remote.WithTransport(srv.Client().Transport)}

	pushed := seedImage(t, host, "test/pushed", "v1", opts...)
	if err := remote.Write(mustRef(t, host+"/test/stored:v1"), stored, opts...); err != nil {
		t.Errorf("re-push of stored image: %v", err)
	}

	pushedDigest, _ := pushed.Digest()
	desc, err := remote.Head(mustRef(t, host+"/test/pushed:v1"), opts...)
	if err != nil || desc.Digest != pushedDigest {
		t.Fatalf("pushed image not served: %v", err)
	}
	if entries, _ := os.ReadDir(rootDir); len(entries) == 0 {
		t.Error("push should have written to the registry directory")
	}
}

func mustRef(t *testing.T, s string) goname.Reference {
	t.Helper()
	r, err := goname.ParseReference(s, goname.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
