package store

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mitchellh/go-homedir"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/store"
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
// resolveChartCreds tests
// --------------------------------------------------------------------------

func TestResolveChartCreds_BothEmpty(t *testing.T) {
	ch := v1.Chart{Name: "mychart", RepoURL: "https://charts.example.com"}
	u, p, err := resolveChartCreds(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != "" || p != "" {
		t.Errorf("expected empty creds, got username=%q password=%q", u, p)
	}
}

func TestResolveChartCreds_BothSetAndEnvPopulated(t *testing.T) {
	t.Setenv("CHART_TEST_USER", "alice")
	t.Setenv("CHART_TEST_PASS", "s3cr3t")

	ch := v1.Chart{
		Name:        "mychart",
		RepoURL:     "https://charts.example.com",
		UsernameEnv: "CHART_TEST_USER",
		PasswordEnv: "CHART_TEST_PASS",
	}
	u, p, err := resolveChartCreds(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != "alice" {
		t.Errorf("username: got %q, want %q", u, "alice")
	}
	if p != "s3cr3t" {
		t.Errorf("password: got %q, want %q", p, "s3cr3t")
	}
}

func TestResolveChartCreds_OnlyUsernameEnvSet_ReturnsError(t *testing.T) {
	ch := v1.Chart{
		Name:        "mychart",
		RepoURL:     "https://charts.example.com",
		UsernameEnv: "CHART_TEST_USER_ONLY",
		// PasswordEnv intentionally omitted
	}
	_, _, err := resolveChartCreds(ch)
	if err == nil {
		t.Fatal("expected error when only usernameEnv is set, got nil")
	}
	if !strings.Contains(err.Error(), "usernameEnv and passwordEnv must both be set") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveChartCreds_OnlyPasswordEnvSet_ReturnsError(t *testing.T) {
	ch := v1.Chart{
		Name:    "mychart",
		RepoURL: "https://charts.example.com",
		// UsernameEnv intentionally omitted
		PasswordEnv: "CHART_TEST_PASS_ONLY",
	}
	_, _, err := resolveChartCreds(ch)
	if err == nil {
		t.Fatal("expected error when only passwordEnv is set, got nil")
	}
	if !strings.Contains(err.Error(), "usernameEnv and passwordEnv must both be set") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveChartCreds_EnvVarUnset_ReturnsError(t *testing.T) {
	// Ensure the env vars are definitely absent.
	t.Setenv("CHART_UNSET_USER", "")
	t.Setenv("CHART_UNSET_PASS", "")

	ch := v1.Chart{
		Name:        "mychart",
		RepoURL:     "https://charts.example.com",
		UsernameEnv: "CHART_UNSET_USER",
		PasswordEnv: "CHART_UNSET_PASS",
	}
	_, _, err := resolveChartCreds(ch)
	if err == nil {
		t.Fatal("expected error when env vars are empty, got nil")
	}
	if !strings.Contains(err.Error(), "must both be set and non-empty") {
		t.Errorf("unexpected error message: %v", err)
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

func TestProcessContent_Images_v1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/myimage", "v1") // transport not needed... AddImage reads via localhost scheme

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

// --------------------------------------------------------------------------
// processImageTxt tests
// --------------------------------------------------------------------------

// writeImageTxtFile writes lines to a temp file and returns it seeked to the
// start, ready for processImageTxt to consume.
func writeImageTxtFile(t *testing.T, lines string) *os.File {
	t.Helper()
	fi, err := os.CreateTemp(t.TempDir(), "images-*.txt")
	if err != nil {
		t.Fatalf("writeImageTxtFile CreateTemp: %v", err)
	}
	t.Cleanup(func() { fi.Close() })
	if _, err := fi.WriteString(lines); err != nil {
		t.Fatalf("writeImageTxtFile WriteString: %v", err)
	}
	if _, err := fi.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("writeImageTxtFile Seek: %v", err)
	}
	return fi
}

func TestProcessImageTxt_SingleImage(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/txtimage", "v1")

	fi := writeImageTxtFile(t, fmt.Sprintf("%s/myorg/txtimage:v1\n", host))
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processImageTxt(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processImageTxt single image: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/txtimage")
}

func TestProcessImageTxt_MultipleImages(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/alpha", "v1")
	seedImage(t, host, "myorg/beta", "v2")

	content := fmt.Sprintf("%s/myorg/alpha:v1\n%s/myorg/beta:v2\n", host, host)
	fi := writeImageTxtFile(t, content)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processImageTxt(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processImageTxt multiple images: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/alpha")
	assertArtifactInStore(t, s, "myorg/beta")
}

func TestProcessImageTxt_SkipsBlankLinesAndComments(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/commenttest", "v1")

	content := fmt.Sprintf("# this is a comment\n\n%s/myorg/commenttest:v1\n\n# another comment\n", host)
	fi := writeImageTxtFile(t, content)
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processImageTxt(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processImageTxt skip blanks/comments: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/commenttest")
	if n := countArtifactsInStore(t, s); n != 1 {
		t.Errorf("expected 1 artifact, got %d", n)
	}
}

func TestProcessImageTxt_EmptyFile(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fi := writeImageTxtFile(t, "")
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processImageTxt(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processImageTxt empty file: %v", err)
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts for empty file, got %d", n)
	}
}

// --------------------------------------------------------------------------
// SyncCmd --image-txt integration tests
// --------------------------------------------------------------------------

func TestSyncCmd_ImageTxt_LocalFile(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/syncedtxt", "v1")

	txtFile, err := os.CreateTemp(t.TempDir(), "images-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	txtPath := txtFile.Name()
	fmt.Fprintf(txtFile, "%s/myorg/syncedtxt:v1\n", host)
	txtFile.Close()

	o := newSyncOpts(s.Root)
	o.ImageTxt = []string{txtPath}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := SyncCmd(ctx, o, s, rso, ro); err != nil {
		t.Fatalf("SyncCmd ImageTxt LocalFile: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/syncedtxt")
}

func TestSyncCmd_ImageTxt_RemoteFile(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/remotetxt", "v1")

	imageListContent := fmt.Sprintf("%s/myorg/remotetxt:v1\n", host)
	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, imageListContent) //nolint:errcheck
	}))
	t.Cleanup(imageSrv.Close)

	o := newSyncOpts(s.Root)
	o.ImageTxt = []string{imageSrv.URL + "/images.txt"}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := SyncCmd(ctx, o, s, rso, ro); err != nil {
		t.Fatalf("SyncCmd ImageTxt RemoteFile: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/remotetxt")
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

// --------------------------------------------------------------------------
// SyncCmd --dry-run tests
// --------------------------------------------------------------------------

// buildProductManifestImage constructs a synthetic OCI file-artifact image
// containing yamlContent as a single layer. The image uses the same media
// types and AnnotationTitle annotation that storeFile/AddArtifact produce,
// so ExtractCmd extracts the layer to a file named fileName.
func buildProductManifestImage(t *testing.T, fileName string, yamlContent []byte) gcrv1.Image {
	t.Helper()
	fileLayer := static.NewLayer(yamlContent, gvtypes.MediaType(consts.FileLayerMediaType))
	img, err := mutate.Append(empty.Image, mutate.Addendum{
		Layer: fileLayer,
		Annotations: map[string]string{
			ocispec.AnnotationTitle: fileName,
		},
	})
	if err != nil {
		t.Fatalf("buildProductManifestImage mutate.Append: %v", err)
	}
	img = mutate.MediaType(img, gvtypes.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, gvtypes.MediaType(consts.FileLocalConfigMediaType))
	return img
}

// TestSyncCmd_DryRun_Products_PrintsManifestToStdout verifies that when
// DryRun is true the product manifest YAML is written to stdout without
// writing anything to the local store — storeImage is never called.
func TestSyncCmd_DryRun_Products_PrintsManifestToStdout(t *testing.T) {
	ctx := newTestContext(t)
	t.Cleanup(func() { zerolog.SetGlobalLevel(zerolog.InfoLevel) })

	const productName = "testproduct"
	const productVersion = "v1.0.0"
	const manifestFileName = productName + "-manifest.yaml"

	manifestYAML := []byte(`apiVersion: content.hauler.cattle.io/v1
kind: Files
metadata:
  name: testproduct-files
spec:
  files:
    - path: https://example.com/test.sh
`)

	// Seed the product registry with the manifest as a file-artifact OCI image.
	host, rOpts := newLocalhostRegistry(t)
	img := buildProductManifestImage(t, manifestFileName, manifestYAML)
	imgTag, err := name.NewTag(
		fmt.Sprintf("%s/hauler/%s:%s", host, manifestFileName, productVersion),
		name.Insecure,
	)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.Write(imgTag, img, rOpts...); err != nil {
		t.Fatalf("remote.Write product manifest image: %v", err)
	}

	// Redirect os.Stdout to capture what SyncCmd prints during dry-run.
	oldStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		w.Close()
		r.Close()
	})

	o := newSyncOpts(t.TempDir())
	o.Products = []string{fmt.Sprintf("%s=%s", productName, productVersion)}
	o.ProductRegistry = host
	o.DryRun = true
	rso := defaultRootOpts(t.TempDir())
	ro := defaultCliOpts()

	// Pass nil store — dry-run must not touch the store at all.
	syncErr := SyncCmd(ctx, o, nil, rso, ro)

	// Close the write end before reading to unblock io.Copy.
	w.Close()
	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	r.Close()
	os.Stdout = oldStdout

	if syncErr != nil {
		t.Fatalf("SyncCmd dry-run: %v", syncErr)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("dry-run stdout should start with YAML document separator; got:\n%s", got)
	}
	if !strings.Contains(got, "kind: Files") {
		t.Errorf("dry-run stdout missing 'kind: Files'; got:\n%s", got)
	}
	if !strings.Contains(got, "testproduct-files") {
		t.Errorf("dry-run stdout missing manifest name 'testproduct-files'; got:\n%s", got)
	}
}

// --------------------------------------------------------------------------
// resolveImageJobs tests
//
// resolveImageJobs is a pure function (no ctx, no I/O) so these tests exercise
// the ~150 lines of precedence-resolution logic directly, without a store or
// network access.
// --------------------------------------------------------------------------

func TestResolveImageJobs_RegistryRelocation(t *testing.T) {
	tests := []struct {
		name        string
		imageName   string
		local       bool
		cliRegistry string
		annotation  string
		wantName    string
	}{
		{
			name:        "CLI flag wins over annotation",
			imageName:   "rancher/rancher:v2.9",
			cliRegistry: "cli-registry.io",
			annotation:  "annotation-registry.io",
			wantName:    "cli-registry.io/rancher/rancher:v2.9",
		},
		{
			name:       "annotation used when no CLI flag",
			imageName:  "rancher/rancher:v2.9",
			annotation: "annotation-registry.io",
			wantName:   "annotation-registry.io/rancher/rancher:v2.9",
		},
		{
			name:        "relocation skipped when ref already carries a registry",
			imageName:   "ghcr.io/rancher/rancher:v2.9",
			cliRegistry: "cli-registry.io",
			wantName:    "ghcr.io/rancher/rancher:v2.9",
		},
		{
			name:        "relocation skipped entirely when Local is true",
			imageName:   "rancher/rancher:v2.9",
			local:       true,
			cliRegistry: "cli-registry.io",
			wantName:    "rancher/rancher:v2.9",
		},
		{
			name:      "no registry flag or annotation leaves name unchanged",
			imageName: "rancher/rancher:v2.9",
			wantName:  "rancher/rancher:v2.9",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Registry: tc.cliRegistry}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationRegistry] = tc.annotation
			}
			images := []v1.Image{{Name: tc.imageName, Local: tc.local}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if got := jobs[0].img.Name; got != tc.wantName {
				t.Errorf("got name %q, want %q", got, tc.wantName)
			}
		})
	}
}

func TestResolveImageJobs_LocalWithVerificationOptions_ReturnsError(t *testing.T) {
	tests := []struct {
		name  string
		o     *flags.SyncOpts
		a     map[string]string
		image v1.Image
	}{
		{
			name:  "key via CLI",
			o:     &flags.SyncOpts{Key: "/some/key.pub"},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "key via annotation",
			o:     &flags.SyncOpts{},
			a:     map[string]string{consts.ImageAnnotationKey: "/some/key.pub"},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "key via per-image",
			o:     &flags.SyncOpts{},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true, Key: "/some/key.pub"},
		},
		{
			name:  "identity via CLI",
			o:     &flags.SyncOpts{CertIdentity: "someone@example.com"},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "identity via annotation",
			o:     &flags.SyncOpts{},
			a:     map[string]string{consts.ImageAnnotationCertIdentity: "someone@example.com"},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true},
		},
		{
			name:  "identity via per-image",
			o:     &flags.SyncOpts{},
			a:     map[string]string{},
			image: v1.Image{Name: "rancher/rancher:v2.9", Local: true, CertIdentity: "someone@example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jobs, err := resolveImageJobs(tc.o, tc.a, []v1.Image{tc.image})
			if err == nil {
				t.Fatalf("expected error, got nil (jobs=%v)", jobs)
			}
			if !strings.Contains(err.Error(), "--local cannot be combined with cosign verification options") {
				t.Errorf("unexpected error message: %v", err)
			}
			if len(jobs) != 0 {
				t.Errorf("expected no jobs appended, got %d", len(jobs))
			}
		})
	}
}

func TestResolveImageJobs_KeyPrecedence(t *testing.T) {
	homeKey, err := homedir.Expand("~/mykey.pub")
	if err != nil {
		t.Fatalf("homedir.Expand: %v", err)
	}

	tests := []struct {
		name       string
		cliKey     string
		annotation string
		imageKey   string
		wantKey    string
	}{
		{
			name:    "CLI only",
			cliKey:  "/cli/key.pub",
			wantKey: "/cli/key.pub",
		},
		{
			// Annotation only applies when the CLI key is unset — it does not
			// override an explicitly-set CLI key.
			name:       "annotation used when CLI key unset, expanded via homedir",
			annotation: "~/mykey.pub",
			wantKey:    homeKey,
		},
		{
			name:       "per-image overrides annotation and CLI, expanded via homedir",
			cliKey:     "/cli/key.pub",
			annotation: "/annotation/key.pub",
			imageKey:   "~/mykey.pub",
			wantKey:    homeKey,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Key: tc.cliKey}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationKey] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Key: tc.imageKey}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			job := jobs[0]
			if !job.needsPubKey {
				t.Fatalf("expected needsPubKey=true")
			}
			if job.key != tc.wantKey {
				t.Errorf("got key %q, want %q", job.key, tc.wantKey)
			}
		})
	}
}

func TestResolveImageJobs_TlogPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		cliTlog    bool
		annotation string
		imageTlog  bool
		wantTlog   bool
	}{
		{
			name:     "CLI true",
			cliTlog:  true,
			wantTlog: true,
		},
		{
			name:       "annotation true overrides CLI false",
			annotation: "true",
			wantTlog:   true,
		},
		{
			name:      "per-image true overrides annotation/CLI false",
			imageTlog: true,
			wantTlog:  true,
		},
		{
			name:     "all false stays false",
			wantTlog: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Key: "/cli/key.pub", Tlog: tc.cliTlog}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationTlog] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Tlog: tc.imageTlog}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].tlog != tc.wantTlog {
				t.Errorf("got tlog %v, want %v", jobs[0].tlog, tc.wantTlog)
			}
		})
	}
}

func TestResolveImageJobs_PlatformPrecedence(t *testing.T) {
	tests := []struct {
		name          string
		cliPlatform   string
		annotation    string
		imagePlatform string
		want          string
	}{
		{name: "CLI only", cliPlatform: "linux/amd64", want: "linux/amd64"},
		// Annotation only applies when the CLI platform is unset — it does not
		// override an explicitly-set CLI platform.
		{name: "annotation used when CLI platform unset", annotation: "linux/arm64", want: "linux/arm64"},
		{name: "per-image overrides annotation and CLI", cliPlatform: "linux/amd64", annotation: "linux/arm64", imagePlatform: "linux/386", want: "linux/386"},
		{name: "none set stays empty", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Platform: tc.cliPlatform}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationPlatform] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Platform: tc.imagePlatform}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].platform != tc.want {
				t.Errorf("got platform %q, want %q", jobs[0].platform, tc.want)
			}
		})
	}
}

func TestResolveImageJobs_RewritePrecedence(t *testing.T) {
	tests := []struct {
		name         string
		imageRewrite string
		want         string
	}{
		{name: "no per-image rewrite stays empty", want: ""},
		{name: "per-image rewrite propagates", imageRewrite: "myregistry.io/rancher", want: "myregistry.io/rancher"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{}
			a := map[string]string{}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", Rewrite: tc.imageRewrite}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].rewrite != tc.want {
				t.Errorf("got rewrite %q, want %q", jobs[0].rewrite, tc.want)
			}
		})
	}
}

func TestResolveImageJobs_ExcludeExtrasPrecedence(t *testing.T) {
	tests := []struct {
		name             string
		cliExcludeExtras bool
		annotation       string
		imageExclude     bool
		want             bool
	}{
		{name: "CLI true", cliExcludeExtras: true, want: true},
		{name: "annotation true overrides CLI false", annotation: "true", want: true},
		{name: "per-image true overrides annotation/CLI false", imageExclude: true, want: true},
		{name: "all false stays false", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{ExcludeExtras: tc.cliExcludeExtras}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationExcludeExtras] = tc.annotation
			}
			images := []v1.Image{{Name: "rancher/rancher:v2.9", ExcludeExtras: tc.imageExclude}}

			jobs, err := resolveImageJobs(o, a, images)
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if jobs[0].excludeExtras != tc.want {
				t.Errorf("got excludeExtras %v, want %v", jobs[0].excludeExtras, tc.want)
			}
		})
	}
}

func TestResolveImageJobs_NoOptions_MinimalJob(t *testing.T) {
	o := &flags.SyncOpts{}
	images := []v1.Image{{Name: "rancher/rancher:v2.9"}}

	jobs, err := resolveImageJobs(o, nil, images)
	if err != nil {
		t.Fatalf("resolveImageJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.img.Name != "rancher/rancher:v2.9" {
		t.Errorf("got name %q", job.img.Name)
	}
	if job.local {
		t.Errorf("expected local=false")
	}
	if job.needsPubKey || job.needsKeyless {
		t.Errorf("expected no verification needed, got needsPubKey=%v needsKeyless=%v", job.needsPubKey, job.needsKeyless)
	}
	if job.platform != "" || job.rewrite != "" || job.excludeExtras {
		t.Errorf("expected zero-value platform/rewrite/excludeExtras, got platform=%q rewrite=%q excludeExtras=%v", job.platform, job.rewrite, job.excludeExtras)
	}
}

func TestResolveImageJobs_Local_NoOptions_AppendsLocalJob(t *testing.T) {
	o := &flags.SyncOpts{}
	images := []v1.Image{{Name: "rancher/rancher:v2.9", Local: true, Rewrite: "myregistry.io/rancher"}}

	jobs, err := resolveImageJobs(o, nil, images)
	if err != nil {
		t.Fatalf("resolveImageJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if !job.local {
		t.Errorf("expected local=true")
	}
	if job.rewrite != "myregistry.io/rancher" {
		t.Errorf("got rewrite %q", job.rewrite)
	}
}

// TestRunImageJobs_WithProgress_RendersEscapeCodesAndCompletionLines proves
// that when runImageJobs is given an explicit non-nil progress renderer
// (constructed over a *bytes.Buffer, not real stdout), the resulting output
// contains both the escape-coded erase/redraw sequences and the usual
// "✓ added" completion line for every successful image.
func TestRunImageJobs_WithProgress_RendersEscapeCodesAndCompletionLines(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const n = 3
	var jobs []imageJob
	for i := 0; i < n; i++ {
		repo := fmt.Sprintf("progress%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	ctx := zl.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	progress := log.NewRenderer(&buf)

	if err := runImageJobs(ctx, s, jobs, 2, rso, ro, progress); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected escape-coded progress output somewhere in the buffer, got %q", out)
	}
	if got := strings.Count(out, "✓ added"); got != n {
		t.Errorf("\"✓ added\" appeared %d times, want %d (one per successful image); full output:\n%s", got, n, out)
	}
}

// TestProcessContent_RealGatingDisablesProgress is the regression test that
// matters most: it runs the real processContent path (not runImageJobs
// directly, and not manually passing progress=nil) against a local test
// registry, with the ambient logger bound to a *bytes.Buffer. Because go
// test's real os.Stdout isn't a TTY, log.ShouldShowProgress naturally
// evaluates false inside processContent's call to newSyncProgress, so
// runImageJobs receives progress == nil through the real gating path.
// Asserts the resulting buffer contains zero escape bytes and still
// contains plain "✓ added" completion lines.
//
// The context is built via log.NewLogger(&buf).WithContext(...) rather than
// a raw zerolog.New(&buf) -- log.NewLogger routes through
// zerolog.ConsoleWriter, which is what actually emitted ANSI color codes
// unconditionally in the bug this test guards against. A raw zerolog logger
// writes plain JSON and never exercises ConsoleWriter at all, so it could
// never have caught this regression.
func TestProcessContent_RealGatingDisablesProgress(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "gated/image", "v1", remoteOpts...)

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/gated/image:v1
`, host)

	fi := writeManifestFile(t, manifest)

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent: %v", err)
	}

	out := buf.Bytes()

	if bytes.Contains(out, []byte("\x1b[")) {
		t.Errorf("expected zero ANSI escape bytes under go test's non-terminal stdout, got %q", out)
	}
	if !strings.Contains(buf.String(), "✓ added") {
		t.Errorf("expected a plain \"✓ added\" completion line, got %q", out)
	}
}

// refCountInLine finds the first line in out containing marker and returns
// how many times ref appears in that line (from marker's start to the line's
// end). Used to prove a completion line names its image ref exactly once --
// not once inline in the message and a second time via a structured
// "image=<ref>" field zerolog would otherwise append from a per-job logger.
func refCountInLine(t *testing.T, out, marker, ref string) int {
	t.Helper()
	idx := strings.Index(out, marker)
	if idx == -1 {
		t.Fatalf("expected output to contain %q, got %q", marker, out)
	}
	line := out[idx:]
	if end := strings.Index(line, "\n"); end != -1 {
		line = line[:end]
	}
	return strings.Count(line, ref)
}

// TestRunImageJobs_NoProgress_CompletionLineRefAppearsOnce is the regression
// test for the reported bug: a sync job's "✓ added <ref> ..." completion
// line named its ref twice -- once inline in the message, once via the
// structured "image=<ref>" field runImageJobs attaches to every job's
// logger for attribution. progress is nil here (the non-TTY case): the bug
// was present in this path too, since the field is attached unconditionally
// regardless of whether progress is active.
func TestRunImageJobs_NoProgress_CompletionLineRefAppearsOnce(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "dup/image", "v1", remoteOpts...)
	ref := host + "/dup/image:v1"

	jobs := []imageJob{{img: v1.Image{Name: ref}}}

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runImageJobs(ctx, s, jobs, 1, rso, ro, nil); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	out := buf.String()
	if got := refCountInLine(t, out, "✓ added", ref); got != 1 {
		t.Errorf("ref %q appeared %d times in the completion line, want 1; full output:\n%s", ref, got, out)
	}
}

// TestRunImageJobs_WithProgress_CompletionLineRefAppearsOnce mirrors
// TestRunImageJobs_NoProgress_CompletionLineRefAppearsOnce but with a
// non-nil progress Renderer, proving the fix applies equally to the
// TTY/progress-enabled path.
func TestRunImageJobs_WithProgress_CompletionLineRefAppearsOnce(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "dup/progress", "v1", remoteOpts...)
	ref := host + "/dup/progress:v1"

	jobs := []imageJob{{img: v1.Image{Name: ref}}}

	s := newTestStore(t)
	var buf bytes.Buffer
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	progress := log.NewRenderer(&buf)

	if err := runImageJobs(t.Context(), s, jobs, 1, rso, ro, progress); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	out := buf.String()
	if got := refCountInLine(t, out, "✓ added", ref); got != 1 {
		t.Errorf("ref %q appeared %d times in the completion line, want 1; full output:\n%s", ref, got, out)
	}
}

// TestProcessImageTxt_RealGatingDisablesProgress mirrors
// TestProcessContent_RealGatingDisablesProgress exactly, but drives
// processImageTxt (the image.txt / -i path) instead of processContent (the
// hauler-manifest / -f path). Before this test existed, the -i path's
// gating behavior was only ever confirmed by hand in manual verification
// rounds -- this closes that coverage gap.
func TestProcessImageTxt_RealGatingDisablesProgress(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "gated/txtimage", "v1", remoteOpts...)

	fi := writeImageTxtFile(t, fmt.Sprintf("%s/gated/txtimage:v1\n", host))

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processImageTxt(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processImageTxt: %v", err)
	}

	out := buf.Bytes()

	if bytes.Contains(out, []byte("\x1b[")) {
		t.Errorf("expected zero ANSI escape bytes under go test's non-terminal stdout, got %q", out)
	}
	if !strings.Contains(buf.String(), "✓ added") {
		t.Errorf("expected a plain \"✓ added\" completion line, got %q", out)
	}
}

func TestFormatIOStats(t *testing.T) {
	st := content.IOStatsSnapshot{
		BlobsWritten:       387,
		BlobsCached:        25,
		BlobBytesWritten:   8_100_000_000,
		BlobSemWait:        41200 * time.Millisecond,
		BlobPeakInFlight:   18,
		IndexWrites:        226,
		IndexDurableWrites: 7,
		IndexBytesWritten:  10_300_000,
		IndexLockWait:      3100 * time.Millisecond,
	}

	// BlobPeakInFlight (18) and the ceiling argument (20) are deliberately
	// distinct: if formatIOStats's Sprintf ever transposed the two %d
	// operands, "peak-inflight=18/20" would fail even though both values
	// individually appear elsewhere in the format string. A fixture where
	// both were 20 could not catch that swap.
	got := formatIOStats(st, 20)

	for _, want := range []string{
		"blobs=412",
		"written=387",
		"cached=25",
		"peak-inflight=18/20",
		"blobsem-wait=41.2s",
		"index-writes=226",
		"durable=7",
		"index-lock-wait=3.1s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatIOStats output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("formatIOStats must produce a single line, got:\n%s", got)
	}
}

// --------------------------------------------------------------------------
// resolveFileJobs
// --------------------------------------------------------------------------

func TestResolveFileJobs_OneJobPerFile(t *testing.T) {
	files := []v1.File{
		{Path: "https://example.com/a.sh"},
		{Path: "https://example.com/b.sh", Name: "renamed-b.sh"},
	}

	jobs := resolveFileJobs(files)
	if len(jobs) != 2 {
		t.Fatalf("resolveFileJobs: got %d jobs, want 2", len(jobs))
	}
	if jobs[0].file.Path != files[0].Path {
		t.Errorf("jobs[0].file.Path = %q, want %q", jobs[0].file.Path, files[0].Path)
	}
	if jobs[1].file.Name != "renamed-b.sh" {
		t.Errorf("jobs[1].file.Name = %q, want %q", jobs[1].file.Name, "renamed-b.sh")
	}
}

func TestResolveFileJobs_EmptyInput(t *testing.T) {
	jobs := resolveFileJobs(nil)
	if len(jobs) != 0 {
		t.Errorf("resolveFileJobs(nil): got %d jobs, want 0", len(jobs))
	}
}

// --------------------------------------------------------------------------
// runFileJobs
// --------------------------------------------------------------------------

func TestRunFileJobs_AllSucceed(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	url1 := seedFileInHTTPServer(t, "one.sh", "#!/bin/sh\necho one")
	url2 := seedFileInHTTPServer(t, "two.sh", "#!/bin/sh\necho two")

	jobs := resolveFileJobs([]v1.File{{Path: url1}, {Path: url2}})
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runFileJobs(ctx, s, jobs, 2, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}
	assertArtifactInStore(t, s, "one.sh")
	assertArtifactInStore(t, s, "two.sh")
}

// TestRunFileJobs_ConcurrencyOneVsFour_ProduceEquivalentStores proves that
// the resulting store content (blob digests + index entry refs) is
// identical regardless of --concurrency, comparing sorted sets rather than
// raw index.json bytes since ordering is nondeterministic under
// concurrency.
func TestRunFileJobs_ConcurrencyOneVsFour_ProduceEquivalentStores(t *testing.T) {
	ctx := newTestContext(t)

	var urls []string
	for i := 0; i < 6; i++ {
		urls = append(urls, seedFileInHTTPServer(t, fmt.Sprintf("multi-%d.sh", i), fmt.Sprintf("#!/bin/sh\necho %d", i)))
	}
	var files []v1.File
	for _, u := range urls {
		files = append(files, v1.File{Path: u})
	}

	run := func(concurrency int) *storeSnapshot {
		s := newTestStore(t)
		jobs := resolveFileJobs(files)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()
		if err := runFileJobs(ctx, s, jobs, concurrency, rso, ro, nil); err != nil {
			t.Fatalf("runFileJobs concurrency=%d: %v", concurrency, err)
		}
		return snapshotStore(t, s)
	}

	snap1 := run(1)
	snap4 := run(4)

	if !equalSnapshots(snap1, snap4) {
		t.Errorf("store snapshots differ between concurrency=1 and concurrency=4:\nconcurrency=1: %+v\nconcurrency=4: %+v", snap1, snap4)
	}
}

// storeSnapshot captures the sorted sets that identify a store's content
// independent of index.json's on-disk ordering.
type storeSnapshot struct {
	digests []string
	refs    []string
}

func snapshotStore(t *testing.T, s *store.Layout) *storeSnapshot {
	t.Helper()
	snap := &storeSnapshot{}
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		snap.digests = append(snap.digests, desc.Digest.String())
		snap.refs = append(snap.refs, desc.Annotations[ocispec.AnnotationRefName])
		return nil
	}); err != nil {
		t.Fatalf("snapshotStore walk: %v", err)
	}
	sort.Strings(snap.digests)
	sort.Strings(snap.refs)
	return snap
}

func equalSnapshots(a, b *storeSnapshot) bool {
	if len(a.digests) != len(b.digests) || len(a.refs) != len(b.refs) {
		return false
	}
	for i := range a.digests {
		if a.digests[i] != b.digests[i] {
			return false
		}
	}
	for i := range a.refs {
		if a.refs[i] != b.refs[i] {
			return false
		}
	}
	return true
}

// --------------------------------------------------------------------------
// Dedup acceptance test
// --------------------------------------------------------------------------

func TestRunFileJobs_DedupesDuplicateSourceAcrossEntries(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	var requests int32
	mux := http.NewServeMux()
	mux.HandleFunc("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&requests, 1)
		io.WriteString(w, "#!/bin/sh\necho install") //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url := srv.URL + "/install.sh"
	files := []v1.File{
		{Path: url},
		{Path: url, Name: "rke2-install.sh"},
	}

	jobs := resolveFileJobs(files)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runFileJobs(ctx, s, jobs, 4, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}

	assertArtifactInStore(t, s, "install.sh")
	assertArtifactInStore(t, s, "rke2-install.sh")
	if n := countArtifactsInStore(t, s); n != 2 {
		t.Errorf("expected 2 index entries (one per Files entry), got %d", n)
	}
	// Without dedup, 2 manifest entries pointing at the identical source
	// would cost 2x a single file's request count (4, see below) -- one full
	// fetch cycle per entry. The dedup cache (pkg/artifacts/file/cache.go)
	// collapses that to exactly one shared fetch cycle, holding the total to
	// what a single, non-duplicated file already costs: pkg/layer's
	// FromOpener reads the source once up front to compute digest (and
	// reuses it as diffID, see pkg/layer/layer.go), then content.OCI.WriteBlob's
	// own digest-keyed dedup (writeBlobShared) lets exactly one of the two
	// jobs' writeLayer calls actually stream the blob to disk -- 1 + 1 = 2,
	// not 1 and not 4.
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("expected exactly 2 GET requests (the cost of one file, not two) despite 2 manifest entries sharing the same source, got %d", got)
	}
}

// --------------------------------------------------------------------------
// Error propagation table
// --------------------------------------------------------------------------

func TestRunFileJobs_ErrorPropagation(t *testing.T) {
	tests := []struct {
		name         string
		concurrency  int
		ignoreErrors bool
		wantErr      bool
		wantGoodOK   bool
	}{
		{name: "concurrency=1 ignoreErrors=false", concurrency: 1, ignoreErrors: false, wantErr: true, wantGoodOK: false},
		{name: "concurrency=1 ignoreErrors=true", concurrency: 1, ignoreErrors: true, wantErr: false, wantGoodOK: true},
		{name: "concurrency=4 ignoreErrors=false", concurrency: 4, ignoreErrors: false, wantErr: true, wantGoodOK: false},
		{name: "concurrency=4 ignoreErrors=true", concurrency: 4, ignoreErrors: true, wantErr: false, wantGoodOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext(t)
			s := newTestStore(t)

			goodURL := seedFileInHTTPServer(t, fmt.Sprintf("good-%s.sh", sanitizeName(tt.name)), "#!/bin/sh\necho good")

			files := []v1.File{
				{Path: "http://127.0.0.1:1/unreachable.sh"},
				{Path: goodURL},
			}

			jobs := resolveFileJobs(files)
			rso := defaultRootOpts(s.Root)
			rso.Retries = 1 // avoid RetriesInterval sleeps in this table
			ro := defaultCliOpts()
			ro.IgnoreErrors = tt.ignoreErrors

			err := runFileJobs(ctx, s, jobs, tt.concurrency, rso, ro, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("runFileJobs error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantGoodOK {
				assertArtifactInStore(t, s, fmt.Sprintf("good-%s.sh", sanitizeName(tt.name)))
			}
		})
	}
}

// sanitizeName turns a subtest name into a usable filename fragment. Lower-
// cased because storeFile's stored ref is normalized to lowercase (OCI/Docker
// reference names must be lowercase) -- an unlowercased fragment here would
// never match a substring check against the actual stored ref.
func sanitizeName(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "-", "=", "-").Replace(s))
}

// --------------------------------------------------------------------------
// Retry
// --------------------------------------------------------------------------

func TestRunFileJobs_RetryEventuallySucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}

	ctx := newTestContext(t)
	s := newTestStore(t)

	var gets int32
	mux := http.NewServeMux()
	mux.HandleFunc("/eventual.sh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		if atomic.AddInt32(&gets, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "#!/bin/sh\necho eventual") //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	jobs := resolveFileJobs([]v1.File{{Path: srv.URL + "/eventual.sh"}})
	rso := defaultRootOpts(s.Root)
	rso.Retries = 2
	ro := defaultCliOpts()

	if err := runFileJobs(ctx, s, jobs, 1, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}
	assertArtifactInStore(t, s, "eventual.sh")
}

// --------------------------------------------------------------------------
// Cancellation
// --------------------------------------------------------------------------

// TestRunFileJobs_CancellationAbortsPromptly is the regression test for
// File.compute()'s context.TODO() fix (pkg/artifacts/file/file.go): a slow
// handler that never responds must not block runFileJobs past ctx's
// cancellation.
func TestRunFileJobs_CancellationAbortsPromptly(t *testing.T) {
	block := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow.sh", func(w http.ResponseWriter, r *http.Request) {
		// storeFile's Client.Name(fi.Path) call (deriving the stored ref,
		// before compute()/fetch even starts) issues an HTTP HEAD via
		// getter.Http.Name, which -- unlike Open -- takes no context
		// parameter at all (net/http.Head has no context-aware variant) and
		// so cannot be cancelled. That's a separate, narrower gap than the
		// one this test targets (compute()'s context.TODO() bug and Open's
		// context wiring); block only the GET that actually fetches content,
		// so this test isolates the fetch-cancellation path under test
		// rather than hanging on the unrelated HEAD gap.
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		<-block
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) })

	zl := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(zl.WithContext(context.Background()))
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	s := newTestStore(t)
	jobs := resolveFileJobs([]v1.File{{Path: srv.URL + "/slow.sh"}})
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	done := make(chan error, 1)
	go func() {
		done <- runFileJobs(ctx, s, jobs, 1, rso, ro, nil)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after ctx cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runFileJobs did not return within 5s of ctx cancellation")
	}
}

// --------------------------------------------------------------------------
// Renderer rows
// --------------------------------------------------------------------------

func TestRunFileJobs_WithProgress_RendersEscapeCodesAndCompletionLines(t *testing.T) {
	const n = 3
	var files []v1.File
	for i := 0; i < n; i++ {
		files = append(files, v1.File{Path: seedFileInHTTPServer(t, fmt.Sprintf("progress-%d.sh", i), "#!/bin/sh\necho hi")})
	}

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	ctx := zl.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	progress := log.NewRenderer(&buf)
	jobs := resolveFileJobs(files)

	if err := runFileJobs(ctx, s, jobs, 2, rso, ro, progress); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected escape-coded progress output somewhere in the buffer, got %q", out)
	}
	if got := strings.Count(out, "✓ added"); got != n {
		t.Errorf("\"✓ added\" appeared %d times, want %d; full output:\n%s", got, n, out)
	}
}

func TestRunFileJobs_NoProgress_CompletionLineRefAppearsOnce(t *testing.T) {
	url := seedFileInHTTPServer(t, "dup-ref.sh", "#!/bin/sh\necho dup")

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	jobs := resolveFileJobs([]v1.File{{Path: url}})
	if err := runFileJobs(ctx, s, jobs, 1, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}

	out := buf.String()
	if got := refCountInLine(t, out, "✓ added", "dup-ref.sh"); got != 1 {
		t.Errorf("ref appeared %d times in the completion line, want 1; full output:\n%s", got, out)
	}
}

// countingBlobHandler wraps an http.Handler and increments hits[digest] for
// every GET request to /v2/<repo>/blobs/<digest>, then delegates to the
// wrapped handler.
type countingBlobHandler struct {
	next http.Handler
	mu   sync.Mutex
	hits map[string]int
}

func newCountingBlobHandler(next http.Handler) *countingBlobHandler {
	return &countingBlobHandler{next: next, hits: make(map[string]int)}
}

func (c *countingBlobHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if idx := strings.Index(r.URL.Path, "/blobs/"); idx != -1 {
			digest := r.URL.Path[idx+len("/blobs/"):]
			c.mu.Lock()
			c.hits[digest]++
			c.mu.Unlock()
		}
	}
	c.next.ServeHTTP(w, r)
}

func (c *countingBlobHandler) count(digest string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[digest]
}

// newCountingLocalhostRegistry mirrors newLocalhostRegistry (add_test.go)
// -- listening on "localhost:0" so go-containerregistry auto-selects plain
// HTTP -- but wraps registry.New() in a countingBlobHandler so tests can
// assert on per-digest blob GET counts.
func newCountingLocalhostRegistry(t *testing.T) (host string, remoteOpts []remote.Option, counter *countingBlobHandler) {
	t.Helper()
	counter = newCountingBlobHandler(registry.New())
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("newCountingLocalhostRegistry listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(counter)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts, counter
}

// TestSyncImages_SharedLayerDownloadedOnce is the acceptance test for the
// whole parallelization effort: two images that share a common layer are
// synced concurrently, and the shared layer's blob must be fetched from the
// registry exactly once regardless of --concurrency.
func TestSyncImages_SharedLayerDownloadedOnce(t *testing.T) {
	for _, concurrency := range []int{1, 4} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			host, remoteOpts, counter := newCountingLocalhostRegistry(t)

			sharedLayer, err := random.Layer(2048, gvtypes.OCILayer)
			if err != nil {
				t.Fatalf("random.Layer: %v", err)
			}
			sharedDigest, err := sharedLayer.Digest()
			if err != nil {
				t.Fatalf("sharedLayer.Digest: %v", err)
			}

			img1, err := mutate.AppendLayers(empty.Image, sharedLayer)
			if err != nil {
				t.Fatalf("mutate.AppendLayers img1: %v", err)
			}
			img2, err := mutate.AppendLayers(empty.Image, sharedLayer)
			if err != nil {
				t.Fatalf("mutate.AppendLayers img2: %v", err)
			}

			ref1, err := name.NewTag(host+"/repo1:latest", name.Insecure)
			if err != nil {
				t.Fatalf("NewTag ref1: %v", err)
			}
			ref2, err := name.NewTag(host+"/repo2:latest", name.Insecure)
			if err != nil {
				t.Fatalf("NewTag ref2: %v", err)
			}
			if err := remote.Write(ref1, img1, remoteOpts...); err != nil {
				t.Fatalf("remote.Write img1: %v", err)
			}
			if err := remote.Write(ref2, img2, remoteOpts...); err != nil {
				t.Fatalf("remote.Write img2: %v", err)
			}

			s := newTestStore(t)
			ctx := newTestContext(t)
			jobs := []imageJob{
				{img: v1.Image{Name: host + "/repo1:latest"}},
				{img: v1.Image{Name: host + "/repo2:latest"}},
			}
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()

			if err := runImageJobs(ctx, s, jobs, concurrency, rso, ro, nil); err != nil {
				t.Fatalf("runImageJobs: %v", err)
			}

			assertArtifactInStore(t, s, "repo1")
			assertArtifactInStore(t, s, "repo2")

			hits := counter.count(sharedDigest.String())
			if hits != 1 {
				t.Errorf("shared layer blob GET count = %d, want exactly 1 (concurrency=%d)", hits, concurrency)
			}
		})
	}
}

// TestSyncImages_ConcurrencyOneMatchesSerial syncs the same set of images
// into two independent stores, once at concurrency=1 and once at
// concurrency=4, and asserts the resulting stores contain the same set of
// blob digests and the same set of index entries (ref+kind+digest tuples).
//
// Deliberately NOT a byte-for-byte index.json comparison: sync.Map range
// order plus the resolver's SliceStable 2-way merge make exact index.json
// byte order nondeterministic across runs. Comparing sorted logical entry
// sets instead avoids turning this into a flaky golden-file test.
func TestSyncImages_ConcurrencyOneMatchesSerial(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const nImages = 5
	var jobs []imageJob
	for i := 0; i < nImages; i++ {
		repo := fmt.Sprintf("repo%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	sSerial := newTestStore(t)
	sParallel := newTestStore(t)
	ctx := newTestContext(t)
	ro := defaultCliOpts()

	if err := runImageJobs(ctx, sSerial, jobs, 1, defaultRootOpts(sSerial.Root), ro, nil); err != nil {
		t.Fatalf("runImageJobs (concurrency=1): %v", err)
	}
	if err := runImageJobs(ctx, sParallel, jobs, 4, defaultRootOpts(sParallel.Root), ro, nil); err != nil {
		t.Fatalf("runImageJobs (concurrency=4): %v", err)
	}

	serialBlobs := sortedBlobDigests(t, sSerial.Root)
	parallelBlobs := sortedBlobDigests(t, sParallel.Root)
	if !equalStringSlices(serialBlobs, parallelBlobs) {
		t.Errorf("blob digest sets differ:\nserial:   %v\nparallel: %v", serialBlobs, parallelBlobs)
	}

	serialEntries := sortedIndexEntries(t, sSerial)
	parallelEntries := sortedIndexEntries(t, sParallel)
	if !equalStringSlices(serialEntries, parallelEntries) {
		t.Errorf("index entry sets differ:\nserial:   %v\nparallel: %v", serialEntries, parallelEntries)
	}
}

// TestSyncImages_ErrorPropagation covers fail-fast/ignore-errors semantics
// across concurrency levels: 4 good images plus 1 that 404s.
func TestSyncImages_ErrorPropagation(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const nGood = 4
	var goodJobs []imageJob
	for i := 0; i < nGood; i++ {
		repo := fmt.Sprintf("good%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		goodJobs = append(goodJobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}
	badRef := host + "/does-not-exist:latest"

	for _, concurrency := range []int{1, 4} {
		for _, ignoreErrors := range []bool{false, true} {
			t.Run(fmt.Sprintf("concurrency=%d/ignoreErrors=%v", concurrency, ignoreErrors), func(t *testing.T) {
				jobs := append([]imageJob{}, goodJobs...)
				jobs = append(jobs, imageJob{img: v1.Image{Name: badRef}})

				s := newTestStore(t)
				ctx := newTestContext(t)
				rso := defaultRootOpts(s.Root)
				ro := defaultCliOpts()
				ro.IgnoreErrors = ignoreErrors

				err := runImageJobs(ctx, s, jobs, concurrency, rso, ro, nil)

				if !ignoreErrors {
					if err == nil {
						t.Fatal("runImageJobs: expected error, got nil")
					}
					if !strings.Contains(err.Error(), "does-not-exist") {
						t.Errorf("runImageJobs error = %q, want it to identify the bad image (does-not-exist), not a bare cancellation", err.Error())
					}
					return
				}

				if err != nil {
					t.Fatalf("runImageJobs (ignoreErrors=true): unexpected error: %v", err)
				}
				for i := 0; i < nGood; i++ {
					assertArtifactInStore(t, s, fmt.Sprintf("good%d", i))
				}
			})
		}
	}
}

// TestRunImageJobs_CancelledJobsDoNotLogAddingImage reproduces the log-spam
// bug: with concurrency=1 and a bad ref placed before several good refs, the
// bad job's failure cancels the errgroup's derived context. Jobs queued after
// it never get a chance to call s.AddImage, so they must never log the
// "adding image [...] to the store" INFO line either -- otherwise a failed
// sync of 1 image looks like it attempted all of them.
//
// concurrency=1 makes cancellation deterministic: errgroup.SetLimit(1) means
// each g.Go call blocks acquiring its semaphore slot until the previous job's
// goroutine has fully returned (including cancelling gctx on failure), so
// jobs run strictly in slice order and the good jobs are guaranteed to
// observe the already-cancelled context before doing anything.
func TestRunImageJobs_CancelledJobsDoNotLogAddingImage(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const nGood = 3
	badRef := host + "/does-not-exist:latest"
	jobs := []imageJob{{img: v1.Image{Name: badRef}}}
	for i := 0; i < nGood; i++ {
		repo := fmt.Sprintf("good%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	s := newTestStore(t)
	var buf bytes.Buffer
	// "adding image [...]" now logs at Debug (cmd/hauler/cli/store/add.go),
	// so this test needs Debug-level output visible. Per-logger .Level() is
	// not sufficient on its own: zerolog's Logger.should() gates on
	// max(logger.level, zerolog.GlobalLevel()) -- and GlobalLevel is
	// process-global state that other tests in this package mutate (e.g.
	// sync_test.go's TestSyncCmd_DryRun_Products_PrintsManifestToStdout
	// resets it to InfoLevel in a t.Cleanup). Under -shuffle/-count>1 that
	// can leave the global level at Info before this test runs, silently
	// suppressing Debug output regardless of the per-logger Level call. Save
	// and restore the global level around this test so it's deterministic
	// regardless of what ran before it, and doesn't leave Debug-level
	// pollution behind for tests that run after it.
	prevGlobalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(prevGlobalLevel) })

	zl := zerolog.New(&buf).Level(zerolog.DebugLevel)
	ctx := zl.WithContext(context.Background())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	err := runImageJobs(ctx, s, jobs, 1, rso, ro, nil)
	if err == nil {
		t.Fatal("runImageJobs: expected error, got nil")
	}

	got := strings.Count(buf.String(), "adding image [")
	if got != 1 {
		t.Errorf("\"adding image [\" logged %d times, want exactly 1 (only the failed job should have attempted logging; the %d good jobs queued after it must never start)\nfull log:\n%s", got, nGood, buf.String())
	}
}

// TestSyncImages_IndexCompletenessUnderParallelAdds builds 20 images, syncs
// them at concurrency=8, and asserts the store's on-disk index.json (not
// just the in-memory nameMap) contains all 20 entries after re-opening the
// store fresh.
func TestSyncImages_IndexCompletenessUnderParallelAdds(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const n = 20
	var jobs []imageJob
	for i := 0; i < n; i++ {
		repo := fmt.Sprintf("img%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	s := newTestStore(t)
	ctx := newTestContext(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runImageJobs(ctx, s, jobs, 8, rso, ro, nil); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	if got := countArtifactsInStore(t, s); got != n {
		t.Errorf("countArtifactsInStore (same instance) = %d, want %d", got, n)
	}

	reopened, err := store.NewLayout(s.Root)
	if err != nil {
		t.Fatalf("re-opening store: %v", err)
	}
	if got := countArtifactsInStore(t, reopened); got != n {
		t.Errorf("countArtifactsInStore (freshly re-opened store) = %d, want %d -- entries lost from index.json on disk", got, n)
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func sortedBlobDigests(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, ocispec.ImageBlobsDir, "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func sortedIndexEntries(t *testing.T, s *store.Layout) []string {
	t.Helper()
	var out []string
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		out = append(out, fmt.Sprintf("%s|%s|%s",
			desc.Annotations[ocispec.AnnotationRefName],
			desc.Annotations["kind"],
			desc.Digest.String(),
		))
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// syncBuffer is a mutex-guarded io.Writer + String() buffer. It is
// deliberately a separate lock from the Renderer's own internal mutex, so a
// test goroutine can read the buffer's contents while the Renderer
// concurrently writes to it (from runImageJobs' background goroutine)
// without racing on the underlying storage under `go test -race`.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// blockingManifestHandler delegates every request to next, except that the
// first GET request matching path is held open: it closes entered (so a
// test can deterministically observe that the request has reached the
// server and is now stuck) and then blocks until release is closed. No
// sleeps are involved anywhere in this synchronization.
type blockingManifestHandler struct {
	next    http.Handler
	path    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *blockingManifestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == h.path {
		h.once.Do(func() { close(h.entered) })
		<-h.release
	}
	h.next.ServeHTTP(w, r)
}

// TestRunImageJobs_BeganFiresOnlyAfterSemaphoreAcquired reproduces the bug
// where progress.Began(name) was invoked from the single-threaded outer
// remote-jobs loop, before g.Go(...) had any chance to block acquiring its
// errgroup.SetLimit concurrency slot.
//
// At concurrency=1 with two jobs, job1's manifest fetch (a real HTTP request
// against a local test registry) is held open on a channel -- indefinitely,
// until the test releases it. Because job2's g.Go call cannot acquire the
// single available slot until job1's goroutine finishes and releases it,
// job2's own goroutine cannot have started yet. So if progress already shows
// job2 as in-flight by the time job1's request is observed to have reached
// the server (which requires job1's goroutine to have done real, comparatively
// expensive network I/O), Began(job2) can only have been called from the
// outer loop rather than from inside job2's own goroutine -- proving the bug
// is present.
func TestRunImageJobs_BeganFiresOnlyAfterSemaphoreAcquired(t *testing.T) {
	// Repo/tag names are kept intentionally short (unlike other tests in this
	// package) so that both refs comfortably fit within the Renderer's status
	// line width budget without being truncated away by truncateNameList --
	// this test asserts on the literal presence of job2's ref substring, so
	// truncation would make that assertion meaningless regardless of whether
	// the underlying bug is present.
	const job1Repo = "a1"
	const job1Tag = "t"
	const job2Repo = "b2"
	const job2Tag = "t"

	handler := &blockingManifestHandler{
		path:    "/v2/" + job1Repo + "/manifests/" + job1Tag,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler.next = registry.New()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	remoteOpts := []remote.Option{remote.WithTransport(srv.Client().Transport)}

	seedImage(t, host, job1Repo, job1Tag, remoteOpts...)
	seedImage(t, host, job2Repo, job2Tag, remoteOpts...)

	s := newTestStore(t)
	buf := &syncBuffer{}
	progress := log.NewRenderer(buf)

	ctx := newTestContext(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	jobs := []imageJob{
		{img: v1.Image{Name: host + "/" + job1Repo + ":" + job1Tag}},
		{img: v1.Image{Name: host + "/" + job2Repo + ":" + job2Tag}},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runImageJobs(ctx, s, jobs, 1, rso, ro, progress)
	}()

	select {
	case <-handler.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for job1's manifest request to reach the test registry")
	}

	if strings.Contains(buf.String(), job2Repo) {
		t.Errorf("progress already shows job2 (%q) as in-flight while job1 is still blocked and concurrency=1 -- "+
			"Began(job2) must have fired from the outer loop before job2's goroutine could possibly have acquired "+
			"a semaphore slot\nbuffer contents:\n%s", job2Repo, buf.String())
	}

	close(handler.release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runImageJobs: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for runImageJobs to finish after releasing job1")
	}

	out := buf.String()
	if got := strings.Count(out, "✓ added"); got != 2 {
		t.Errorf("\"✓ added\" appeared %d times, want 2; full output:\n%s", got, out)
	}
}
