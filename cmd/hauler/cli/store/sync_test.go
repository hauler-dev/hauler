package store

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"hauler.dev/go/hauler/internal/flags"
)

// writeManifestFile writes yamlContent to a temp file, seeks back to the
// start, and registers t.Cleanup to close + remove it. Returns the open
// *os.File, ready for processContent to read.
func writeManifestFile(t *testing.T, yamlContent string) *os.File {
	t.Helper()
	fi, err := os.CreateTemp(t.TempDir(), "hauler-manifest-*.yaml")
	if err != nil {
		t.Fatalf("writeManifestFile CreateTemp: %v", err)
	}
	t.Cleanup(func() { fi.Close() })
	if _, err := fi.WriteString(yamlContent); err != nil {
		t.Fatalf("writeManifestFile WriteString: %v", err)
	}
	if _, err := fi.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("writeManifestFile Seek: %v", err)
	}
	return fi
}

// newSyncOpts builds a SyncOpts pointing at storeDir.
func newSyncOpts(storeDir string) *flags.SyncOpts {
	return &flags.SyncOpts{
		StoreRootOpts: defaultRootOpts(storeDir),
	}
}

// --------------------------------------------------------------------------
// processContent tests
// --------------------------------------------------------------------------

func TestProcessContent_Files_v1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileURL := seedFileInHTTPServer(t, "synced.sh", "#!/bin/sh\necho hello")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Files
metadata:
  name: test-files
spec:
  files:
    - path: %s
`, fileURL)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent Files v1: %v", err)
	}
	assertArtifactInStore(t, s, "synced.sh")
}

func TestProcessContent_Files_v1alpha1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileURL := seedFileInHTTPServer(t, "legacy.sh", "#!/bin/sh\necho legacy")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1alpha1
kind: Files
metadata:
  name: test-files-alpha
spec:
  files:
    - path: %s
`, fileURL)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent Files v1alpha1: %v", err)
	}
	assertArtifactInStore(t, s, "legacy.sh")
}

func TestProcessContent_Charts_v1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// Use the same relative path as add_test.go: url.ParseRequestURI accepts
	// absolute Unix paths, making isUrl() return true for them. A relative
	// path correctly keeps isUrl() false so Helm sees it as a local directory.
	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Charts
metadata:
  name: test-charts
spec:
  charts:
    - name: rancher-cluster-templates-0.5.2.tgz
      repoURL: %s
`, chartTestdataDir)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent Charts v1: %v", err)
	}
	assertArtifactInStore(t, s, "rancher-cluster-templates")
}

func TestProcessContent_Charts_v1alpha1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1alpha1
kind: Charts
metadata:
  name: test-charts-alpha
spec:
  charts:
    - name: rancher-cluster-templates-0.5.2.tgz
      repoURL: %s
`, chartTestdataDir)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent Charts v1alpha1: %v", err)
	}
	assertArtifactInStore(t, s, "rancher-cluster-templates")
}

func TestProcessContent_Images_v1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/myimage", "v1") // transport not needed; AddImage reads via localhost scheme

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/myorg/myimage:v1
`, host)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent Images v1: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/myimage")
}

func TestProcessContent_Images_v1alpha1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/legacyimage", "v1")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1alpha1
kind: Images
metadata:
  name: test-images-alpha
spec:
  images:
    - name: %s/myorg/legacyimage:v1
`, host)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent Images v1alpha1: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/legacyimage")
}

func TestProcessContent_UnsupportedKind(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// A valid apiVersion with an unsupported kind passes content.Load but hits
	// the default branch of the kind switch, returning an error.
	manifest := `apiVersion: content.hauler.cattle.io/v1
kind: Unknown
metadata:
  name: test
`

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err == nil {
		t.Fatal("expected error for unsupported kind, got nil")
	}
}

func TestProcessContent_UnsupportedVersion(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// An unrecognized apiVersion causes content.Load to return an error, which
	// processContent treats as a warn-and-skip — the function returns nil and
	// no artifact is added to the store.
	manifest := `apiVersion: content.hauler.cattle.io/v2
kind: Files
metadata:
  name: test
spec:
  files:
    - path: /dev/null
`

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("expected nil for unrecognized apiVersion (warn-and-skip), got: %v", err)
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after skipped document, got %d", n)
	}
}

func TestProcessContent_MultiDoc(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileURL := seedFileInHTTPServer(t, "multi.sh", "#!/bin/sh\necho multi")
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/multiimage", "v1")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Files
metadata:
  name: test-files
spec:
  files:
    - path: %s
---
apiVersion: content.hauler.cattle.io/v1
kind: Charts
metadata:
  name: test-charts
spec:
  charts:
    - name: rancher-cluster-templates-0.5.2.tgz
      repoURL: %s
---
apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/myorg/multiimage:v1
`, fileURL, chartTestdataDir, host)

	fi := writeManifestFile(t, manifest)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent MultiDoc: %v", err)
	}
	assertArtifactInStore(t, s, "multi.sh")
	assertArtifactInStore(t, s, "rancher-cluster-templates")
	assertArtifactInStore(t, s, "myorg/multiimage")
}

// --------------------------------------------------------------------------
// SyncCmd integration tests
// --------------------------------------------------------------------------

func TestSyncCmd_LocalFile(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileURL := seedFileInHTTPServer(t, "synced-local.sh", "#!/bin/sh\necho local")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Files
metadata:
  name: test-sync-local
spec:
  files:
    - path: %s
`, fileURL)

	// SyncCmd reads by file path, so write and close the manifest file first.
	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-local-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	o := newSyncOpts(s.Root)
	o.FileName = []string{manifestPath}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := SyncCmd(ctx, o, s, rso, ro); err != nil {
		t.Fatalf("SyncCmd LocalFile: %v", err)
	}
	assertArtifactInStore(t, s, "synced-local.sh")
}

func TestSyncCmd_RemoteManifest(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileURL := seedFileInHTTPServer(t, "synced-remote.sh", "#!/bin/sh\necho remote")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Files
metadata:
  name: test-sync-remote
spec:
  files:
    - path: %s
`, fileURL)

	// Serve the manifest itself over HTTP so SyncCmd's remote-download path is exercised.
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		io.WriteString(w, manifest) //nolint:errcheck
	}))
	t.Cleanup(manifestSrv.Close)

	o := newSyncOpts(s.Root)
	o.FileName = []string{manifestSrv.URL + "/manifest.yaml"}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := SyncCmd(ctx, o, s, rso, ro); err != nil {
		t.Fatalf("SyncCmd RemoteManifest: %v", err)
	}
	assertArtifactInStore(t, s, "synced-remote.sh")
}
