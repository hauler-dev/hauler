package store

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// --------------------------------------------------------------------------
// Unique test helpers for sync tests
// --------------------------------------------------------------------------

// writeManifestFile writes yamlContent to a temp file, seeks back to the
// start, and registers t.Cleanup to close + remove it.
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

// writeImageTxtFile writes lines to a temp file and returns it seeked to the
// start, ready for processImageTxtPublic to consume.
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

// buildProductManifestImage constructs a synthetic OCI file-artifact image
// containing yamlContent as a single layer.
func buildProductManifestImage(t *testing.T, fileName string, yamlContent []byte) gcrv1.Image {
	t.Helper()
	fileLayer := static.NewLayer(yamlContent, gvtypes.MediaType("application/vnd.hauler.file.layer.v1.tar+gzip"))
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
	img = mutate.ConfigMediaType(img, gvtypes.MediaType("application/vnd.hauler.file.config.v1+json"))
	return img
}

// --------------------------------------------------------------------------
// SyncOptions.Validate tests
// --------------------------------------------------------------------------

func TestSyncOptionsValidate_NoMode(t *testing.T) {
	opts := SyncOptions{}
	err := opts.Validate()
	if err == nil {
		t.Fatal("expected error for no input mode, got nil")
	}
	if !strings.Contains(err.Error(), "exactly one input mode") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSyncOptionsValidate_TwoModes(t *testing.T) {
	opts := SyncOptions{
		Products: []string{"rancher=v2.10.1"},
		FileName: []string{"manifest.yaml"},
	}
	err := opts.Validate()
	if err == nil {
		t.Fatal("expected error for two input modes, got nil")
	}
	if !strings.Contains(err.Error(), "only one input mode") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSyncOptionsValidate_DryRunNoProducts(t *testing.T) {
	opts := SyncOptions{
		FileName: []string{"manifest.yaml"},
		DryRun:   true,
	}
	err := opts.Validate()
	if err == nil {
		t.Fatal("expected error for DryRun without Products, got nil")
	}
	if !strings.Contains(err.Error(), "DryRun is only valid with Products") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestSyncOptionsValidate_ProductsOnly(t *testing.T) {
	opts := SyncOptions{
		Products: []string{"rancher=v2.10.1"},
	}
	err := opts.Validate()
	if err != nil {
		t.Fatalf("expected no error for Products only, got: %v", err)
	}
}

func TestSyncOptionsValidate_FileNameOnly(t *testing.T) {
	opts := SyncOptions{
		FileName: []string{"manifest.yaml"},
	}
	err := opts.Validate()
	if err != nil {
		t.Fatalf("expected no error for FileName only, got: %v", err)
	}
}

func TestSyncOptionsValidate_ImageTxtOnly(t *testing.T) {
	opts := SyncOptions{
		ImageTxt: []string{"images.txt"},
	}
	err := opts.Validate()
	if err != nil {
		t.Fatalf("expected no error for ImageTxt only, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// Sync integration tests
// --------------------------------------------------------------------------

func TestSync_LocalFile(t *testing.T) {
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

	opts := SyncOptions{
		FileName: []string{manifestPath},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync LocalFile: %v", err)
	}
	assertArtifactInStore(t, s, "synced-local.sh")
}

func TestSync_RemoteManifest(t *testing.T) {
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

	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		io.WriteString(w, manifest) //nolint:errcheck
	}))
	t.Cleanup(manifestSrv.Close)

	opts := SyncOptions{
		FileName: []string{manifestSrv.URL + "/manifest.yaml"},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync RemoteManifest: %v", err)
	}
	assertArtifactInStore(t, s, "synced-remote.sh")
}

func TestSync_ImageTxt_LocalFile(t *testing.T) {
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

	opts := SyncOptions{
		ImageTxt: []string{txtPath},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync ImageTxt LocalFile: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/syncedtxt")
}

func TestSync_ImageTxt_RemoteFile(t *testing.T) {
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

	opts := SyncOptions{
		ImageTxt: []string{imageSrv.URL + "/images.txt"},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync ImageTxt RemoteFile: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/remotetxt")
}

func TestSync_MultiDoc(t *testing.T) {
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
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/myorg/multiimage:v1
`, fileURL, host)

	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-multi-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	opts := SyncOptions{
		FileName: []string{manifestPath},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync MultiDoc: %v", err)
	}
	assertArtifactInStore(t, s, "multi.sh")
	assertArtifactInStore(t, s, "myorg/multiimage")
}

func TestSync_UnsupportedKind(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	manifest := `apiVersion: content.hauler.cattle.io/v1
kind: Unknown
metadata:
  name: test
`
	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-unknown-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	opts := SyncOptions{
		FileName: []string{manifestPath},
	}
	if err := Sync(ctx, s, opts); err == nil {
		t.Fatal("expected error for unsupported kind, got nil")
	}
}

func TestSync_UnsupportedVersion(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// An unrecognized apiVersion causes content.Load to return an error, which
	// processContentPublic treats as a warn-and-skip.
	manifest := `apiVersion: content.hauler.cattle.io/v2
kind: Files
metadata:
  name: test
spec:
  files:
    - path: /dev/null
`
	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-version-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	opts := SyncOptions{
		FileName: []string{manifestPath},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("expected nil for unrecognized apiVersion (warn-and-skip), got: %v", err)
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after skipped document, got %d", n)
	}
}

func TestSync_ImagesWithRegistry(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/registrytest", "v1")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-images-registry
  annotations:
    hauler.dev/registry: %s
spec:
  images:
    - name: myorg/registrytest:v1
`, host)

	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-registry-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	opts := SyncOptions{
		FileName: []string{manifestPath},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync ImagesWithRegistry: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/registrytest")
}

// --------------------------------------------------------------------------
// processContentPublic tests
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
	opts := &SyncOptions{}

	if err := processContentPublic(ctx, fi, opts, s); err != nil {
		t.Fatalf("processContent Files v1: %v", err)
	}
	assertArtifactInStore(t, s, "synced.sh")
}

func TestProcessContent_Images_v1(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/myimage", "v1")

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/myorg/myimage:v1
`, host)

	fi := writeManifestFile(t, manifest)
	opts := &SyncOptions{}

	if err := processContentPublic(ctx, fi, opts, s); err != nil {
		t.Fatalf("processContent Images v1: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/myimage")
}

func TestProcessContent_UnsupportedKind(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	manifest := `apiVersion: content.hauler.cattle.io/v1
kind: Unknown
metadata:
  name: test
`

	fi := writeManifestFile(t, manifest)
	opts := &SyncOptions{}

	if err := processContentPublic(ctx, fi, opts, s); err == nil {
		t.Fatal("expected error for unsupported kind, got nil")
	}
}

func TestProcessContent_UnsupportedVersion(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	manifest := `apiVersion: content.hauler.cattle.io/v2
kind: Files
metadata:
  name: test
spec:
  files:
    - path: /dev/null
`

	fi := writeManifestFile(t, manifest)
	opts := &SyncOptions{}

	if err := processContentPublic(ctx, fi, opts, s); err != nil {
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
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/myorg/multiimage:v1
`, fileURL, host)

	fi := writeManifestFile(t, manifest)
	opts := &SyncOptions{}

	if err := processContentPublic(ctx, fi, opts, s); err != nil {
		t.Fatalf("processContent MultiDoc: %v", err)
	}
	assertArtifactInStore(t, s, "multi.sh")
	assertArtifactInStore(t, s, "myorg/multiimage")
}

// --------------------------------------------------------------------------
// processImageTxtPublic tests
// --------------------------------------------------------------------------

func TestProcessImageTxt_SingleImage(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "myorg/txtimage", "v1")

	fi := writeImageTxtFile(t, fmt.Sprintf("%s/myorg/txtimage:v1\n", host))
	opts := &SyncOptions{}

	if err := processImageTxtPublic(ctx, fi, opts, s); err != nil {
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
	opts := &SyncOptions{}

	if err := processImageTxtPublic(ctx, fi, opts, s); err != nil {
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
	opts := &SyncOptions{}

	if err := processImageTxtPublic(ctx, fi, opts, s); err != nil {
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
	opts := &SyncOptions{}

	if err := processImageTxtPublic(ctx, fi, opts, s); err != nil {
		t.Fatalf("processImageTxt empty file: %v", err)
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts for empty file, got %d", n)
	}
}

// --------------------------------------------------------------------------
// ChartsWithAddImages test
// --------------------------------------------------------------------------

func TestSync_ChartsWithAddImages(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// Use the testdata chart which has images in its templates
	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Charts
metadata:
  name: test-charts-add-images
spec:
  charts:
    - name: rancher-cluster-templates-0.5.2.tgz
      repoURL: %s
      addImages: true
`, chartTestdataDir)

	manifestFile, err := os.CreateTemp(t.TempDir(), "hauler-sync-charts-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	manifestPath := manifestFile.Name()
	if _, err := manifestFile.WriteString(manifest); err != nil {
		manifestFile.Close()
		t.Fatalf("WriteString: %v", err)
	}
	manifestFile.Close()

	opts := SyncOptions{
		FileName: []string{manifestPath},
	}
	if err := Sync(ctx, s, opts); err != nil {
		t.Fatalf("Sync ChartsWithAddImages: %v", err)
	}
	assertArtifactInStore(t, s, "rancher-cluster-templates")
}

// --------------------------------------------------------------------------
// Unused imports suppression
// --------------------------------------------------------------------------

var (
	_ = random.Image
	_ = remote.Option(nil)
)
