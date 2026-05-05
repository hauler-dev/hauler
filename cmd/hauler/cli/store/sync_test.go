package store

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/consts"
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
