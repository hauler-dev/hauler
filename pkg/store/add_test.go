package store

import (
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"

	"hauler.dev/go/hauler/pkg/consts"

	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
)

// --------------------------------------------------------------------------
// Test helpers not in save_test.go
// --------------------------------------------------------------------------

// newLocalhostRegistry creates an in-memory OCI registry server listening on
// localhost (rather than 127.0.0.1) so go-containerregistry's Scheme() method
// automatically selects plain HTTP for "localhost:PORT/…" refs.
func newLocalhostRegistry(t *testing.T) (host string, remoteOpts []remote.Option) {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("newLocalhostRegistry listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(registry.New())
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts
}

// seedCosignV2Artifacts pushes synthetic cosign v2 signature, attestation, and SBOM
// manifests at the sha256-<hex>.sig / .att / .sbom tags derived from baseImg's digest.
func seedCosignV2Artifacts(t *testing.T, host, repo string, baseImg gcrv1.Image, opts ...remote.Option) {
	t.Helper()
	hash, err := baseImg.Digest()
	if err != nil {
		t.Fatalf("seedCosignV2Artifacts: get digest: %v", err)
	}
	tagPrefix := strings.ReplaceAll(hash.String(), ":", "-")
	for _, suffix := range []string{".sig", ".att", ".sbom"} {
		img, err := random.Image(64, 1)
		if err != nil {
			t.Fatalf("seedCosignV2Artifacts: random.Image (%s): %v", suffix, err)
		}
		ref, err := name.NewTag(host+"/"+repo+":"+tagPrefix+suffix, name.Insecure)
		if err != nil {
			t.Fatalf("seedCosignV2Artifacts: NewTag (%s): %v", suffix, err)
		}
		if err := remote.Write(ref, img, opts...); err != nil {
			t.Fatalf("seedCosignV2Artifacts: Write (%s): %v", suffix, err)
		}
	}
}

// seedOCI11Referrer pushes a synthetic OCI 1.1 / cosign v3 Sigstore bundle manifest
// whose subject field points at baseImg.
func seedOCI11Referrer(t *testing.T, host, repo string, baseImg gcrv1.Image, opts ...remote.Option) {
	t.Helper()
	hash, err := baseImg.Digest()
	if err != nil {
		t.Fatalf("seedOCI11Referrer: get digest: %v", err)
	}
	rawManifest, err := baseImg.RawManifest()
	if err != nil {
		t.Fatalf("seedOCI11Referrer: raw manifest: %v", err)
	}
	mt, err := baseImg.MediaType()
	if err != nil {
		t.Fatalf("seedOCI11Referrer: media type: %v", err)
	}
	baseDesc := gcrv1.Descriptor{
		MediaType: mt,
		Digest:    hash,
		Size:      int64(len(rawManifest)),
	}

	bundleJSON := []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	bundleLayer := static.NewLayer(bundleJSON, gvtypes.MediaType(consts.SigstoreBundleMediaType))
	referrerImg, err := mutate.AppendLayers(empty.Image, bundleLayer)
	if err != nil {
		t.Fatalf("seedOCI11Referrer: AppendLayers: %v", err)
	}
	referrerImg = mutate.MediaType(referrerImg, gvtypes.OCIManifestSchema1)
	referrerImg = mutate.ConfigMediaType(referrerImg, gvtypes.MediaType(consts.OCIEmptyConfigMediaType))
	referrerImg = mutate.Subject(referrerImg, baseDesc).(gcrv1.Image)

	referrerTag, err := name.NewTag(host+"/"+repo+":bundle-referrer", name.Insecure)
	if err != nil {
		t.Fatalf("seedOCI11Referrer: NewTag: %v", err)
	}
	if err := remote.Write(referrerTag, referrerImg, opts...); err != nil {
		t.Fatalf("seedOCI11Referrer: Write: %v", err)
	}
}

// seedStoreDescriptor injects a descriptor with the given annotations directly
// into the store index without requiring a real registry or blob.
func seedStoreDescriptor(t *testing.T, s *Layout, annotations map[string]string) {
	t.Helper()
	desc := ocispec.Descriptor{
		MediaType:   ocispec.MediaTypeImageManifest,
		Digest:      digest.Digest("sha256:" + strings.Repeat("a", 64)),
		Size:        1,
		Annotations: annotations,
	}
	if err := s.OCI.AddIndex(desc); err != nil {
		t.Fatalf("seedStoreDescriptor: %v", err)
	}
}

// assertArtifactInStore walks the store and fails the test if no descriptor
// has an AnnotationRefName containing refSubstring.
func assertArtifactInStore(t *testing.T, s *Layout, refSubstring string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if desc.Annotations != nil {
			ref := desc.Annotations[ocispec.AnnotationRefName]
			if strings.Contains(ref, refSubstring) {
				found = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("assertArtifactInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no artifact with ref containing %q found in store", refSubstring)
	}
}

// assertArtifactKindInStore walks the store and fails if no descriptor has an
// AnnotationRefName containing refSubstring AND KindAnnotationName equal to kind.
func assertArtifactKindInStore(t *testing.T, s *Layout, refSubstring, kind string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if desc.Annotations != nil &&
			strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) &&
			desc.Annotations[consts.KindAnnotationName] == kind {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("assertArtifactKindInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no artifact with ref containing %q and kind %q found in store", refSubstring, kind)
	}
}

// countArtifactsInStore returns the number of descriptors in the store index.
func countArtifactsInStore(t *testing.T, s *Layout) int {
	t.Helper()
	count := 0
	if err := s.OCI.Walk(func(_ string, _ ocispec.Descriptor) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("countArtifactsInStore walk: %v", err)
	}
	return count
}

// assertAnnotationsInStore walks the store and fails if no descriptor has both
// AnnotationRefName == refName AND ContainerdImageNameKey == containerdName.
func assertAnnotationsInStore(t *testing.T, s *Layout, refName, containerdName string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if desc.Annotations != nil &&
			desc.Annotations[ocispec.AnnotationRefName] == refName &&
			desc.Annotations[consts.ContainerdImageNameKey] == containerdName {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("assertAnnotationsInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no artifact with AnnotationRefName=%q and ContainerdImageNameKey=%q found in store", refName, containerdName)
	}
}

// --------------------------------------------------------------------------
// AddFile tests
// --------------------------------------------------------------------------

func TestAddFile(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("local file stored successfully", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "testfile-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		tmp.WriteString("hello hauler") //nolint:errcheck
		tmp.Close()

		s := newTestStore(t)
		if err := AddFile(ctx, s, v1.File{Path: tmp.Name()}); err != nil {
			t.Fatalf("AddFile: %v", err)
		}
		assertArtifactInStore(t, s, filepath.Base(tmp.Name()))
	})

	t.Run("HTTP URL stored under basename", func(t *testing.T) {
		url := seedFileInHTTPServer(t, "script.sh", "#!/bin/sh\necho ok")
		s := newTestStore(t)
		if err := AddFile(ctx, s, v1.File{Path: url}); err != nil {
			t.Fatalf("AddFile: %v", err)
		}
		assertArtifactInStore(t, s, "script.sh")
	})

	t.Run("name override changes stored ref", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "orig-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		tmp.Close()

		s := newTestStore(t)
		if err := AddFile(ctx, s, v1.File{Path: tmp.Name(), Name: "custom.sh"}); err != nil {
			t.Fatalf("AddFile: %v", err)
		}
		assertArtifactInStore(t, s, "custom.sh")
	})

	t.Run("nonexistent local path returns error", func(t *testing.T) {
		s := newTestStore(t)
		err := AddFile(ctx, s, v1.File{Path: "/nonexistent/path/missing-file.txt"})
		if err == nil {
			t.Fatal("expected error for nonexistent path, got nil")
		}
	})
}

// --------------------------------------------------------------------------
// AddImageWithOpts tests
// --------------------------------------------------------------------------

func TestAddImageWithOpts_ValidRef(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "test/repo", "v1", rOpts...)

	s := newTestStore(t)
	err := AddImageWithOpts(ctx, s, host+"/test/repo:v1", ImageAddOptions{})
	if err != nil {
		t.Fatalf("AddImageWithOpts: %v", err)
	}
	assertArtifactInStore(t, s, "test/repo:v1")
}

func TestAddImageWithOpts_InvalidRef(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	err := AddImageWithOpts(ctx, s, "INVALID IMAGE REF !! ##", ImageAddOptions{})
	if err == nil {
		t.Fatal("expected error for invalid ref, got nil")
	}
}

func TestAddImageWithOpts_IgnoreErrors(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	s := newTestStore(t)
	err := AddImageWithOpts(ctx, s, host+"/nonexistent/image:missing", ImageAddOptions{IgnoreErrors: true})
	if err != nil {
		t.Fatalf("expected nil with IgnoreErrors=true, got: %v", err)
	}
}

func TestAddImageWithOpts_PlatformFilter(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedIndex(t, host, "test/multiarch", "v1", rOpts...)

	s := newTestStore(t)
	err := AddImageWithOpts(ctx, s, host+"/test/multiarch:v1", ImageAddOptions{Platform: "linux/amd64"})
	if err != nil {
		t.Fatalf("AddImageWithOpts with platform filter: %v", err)
	}
	// Platform filter resolves a single manifest from the index to a single image.
	assertArtifactKindInStore(t, s, "test/multiarch:v1", consts.KindAnnotationImage)
}

func TestAddImageWithOpts_ExcludeExtras(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img := seedImage(t, host, "test/signed", "v1", rOpts...)
	seedCosignV2Artifacts(t, host, "test/signed", img, rOpts...)

	s := newTestStore(t)
	err := AddImageWithOpts(ctx, s, host+"/test/signed:v1", ImageAddOptions{ExcludeExtras: true})
	if err != nil {
		t.Fatalf("AddImageWithOpts with excludeExtras: %v", err)
	}

	// Only the primary image must be present - no sigs, atts, or sboms.
	count := countArtifactsInStore(t, s)
	if count != 1 {
		t.Errorf("expected 1 artifact in store, got %d", count)
	}
	assertArtifactKindInStore(t, s, "test/signed:v1", consts.KindAnnotationImage)

	// Verify no sig/att/sbom kind annotations are present.
	for _, kind := range []string{consts.KindAnnotationSigs, consts.KindAnnotationAtts, consts.KindAnnotationSboms} {
		found := false
		if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
			if desc.Annotations != nil && desc.Annotations[consts.KindAnnotationName] == kind {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatalf("walk: %v", err)
		}
		if found {
			t.Errorf("unexpected artifact with kind %q found in store", kind)
		}
	}
}

func TestAddImageWithOpts_Rewrite(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "src/repo", "v1", rOpts...)

	s := newTestStore(t)
	err := AddImageWithOpts(ctx, s, host+"/src/repo:v1", ImageAddOptions{
		Rewrite:    "newrepo/img:v2",
		RawRewrite: "newrepo/img:v2",
	})
	if err != nil {
		t.Fatalf("AddImageWithOpts with rewrite: %v", err)
	}
	assertArtifactInStore(t, s, "newrepo/img:v2")
}

func TestAddImageWithOpts_RewriteDigestRef(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	img := seedImage(t, host, "src/repo", "digest-src", rOpts...)
	h, err := img.Digest()
	if err != nil {
		t.Fatalf("img.Digest: %v", err)
	}

	s := newTestStore(t)
	digestRef := host + "/src/repo@" + h.String()
	err = AddImageWithOpts(ctx, s, digestRef, ImageAddOptions{
		Rewrite:    "newrepo/img",
		RawRewrite: "newrepo/img",
	})
	if err == nil {
		t.Fatal("expected error for digest ref rewrite without explicit tag, got nil")
	}
	if !strings.Contains(err.Error(), "cannot rewrite digest reference") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --------------------------------------------------------------------------
// RewriteReference tests
// --------------------------------------------------------------------------

func TestRewriteReference_Valid(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "src/repo", "v1", rOpts...)

	s := newTestStore(t)
	if err := s.AddImage(ctx, host+"/src/repo:v1", "", false, rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	oldRef, err := name.NewTag(host+"/src/repo:v1", name.Insecure)
	if err != nil {
		t.Fatalf("parse oldRef: %v", err)
	}
	newRef, err := name.NewTag(host+"/dst/repo:v2", name.Insecure)
	if err != nil {
		t.Fatalf("parse newRef: %v", err)
	}

	rawRewrite := newRef.String()

	if err := RewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
		t.Fatalf("RewriteReference: %v", err)
	}

	assertArtifactInStore(t, s, "dst/repo:v2")
}

func TestRewriteReference_NotFound(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	oldRef, _ := name.NewTag("docker.io/missing/repo:v1")
	newRef, _ := name.NewTag("docker.io/new/repo:v2")
	rawRewrite := newRef.String()

	err := RewriteReference(ctx, s, oldRef, newRef, rawRewrite)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not find") {
		t.Errorf("expected 'could not find' in error, got: %v", err)
	}
}

func TestRewriteReference_LibraryStrip(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	seedStoreDescriptor(t, s, map[string]string{
		ocispec.AnnotationRefName:     "library/nginx:latest",
		consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
	})

	oldRef, _ := name.NewTag("nginx:latest")
	newRef, _ := name.NewTag("nginx:v2")
	rawRewrite := "nginx:v2"

	if err := RewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
		t.Fatalf("RewriteReference: %v", err)
	}
	// library/ must be stripped; registry stays index.docker.io
	assertAnnotationsInStore(t, s, "nginx:v2", "index.docker.io/nginx:v2")
}

func TestRewriteReference_ExplicitDockerIO(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	seedStoreDescriptor(t, s, map[string]string{
		ocispec.AnnotationRefName:     "library/nginx:latest",
		consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
	})

	oldRef, _ := name.NewTag("nginx:latest")
	newRef, _ := name.NewTag("docker.io/nginx:v2")
	rawRewrite := "docker.io/nginx:v2"

	if err := RewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
		t.Fatalf("RewriteReference: %v", err)
	}
	// rawRewrite starts with "docker.io" so condition must NOT fire; library/ preserved
	assertAnnotationsInStore(t, s, "library/nginx:v2", "index.docker.io/library/nginx:v2")
}

func TestRewriteReference_NonDockerSource(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "src/repo", "v1", rOpts...)

	s := newTestStore(t)
	if err := s.AddImage(ctx, host+"/src/repo:v1", "", false, rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	oldRef, _ := name.NewTag(host+"/src/repo:v1", name.Insecure)
	newRef, _ := name.NewTag("newrepo/img:v2") // defaults to index.docker.io
	rawRewrite := "newrepo/img:v2"

	if err := RewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
		t.Fatalf("RewriteReference: %v", err)
	}
	// condition fires so registry reverts to host, no library/ to strip
	assertAnnotationsInStore(t, s, "newrepo/img:v2", host+"/newrepo/img:v2")
}

// --------------------------------------------------------------------------
// Chart test helpers
// --------------------------------------------------------------------------

// chartTestdataDir is the relative path from pkg/store/ to the
// top-level testdata directory.
const chartTestdataDir = "../../testdata"

// seedChartWithImages builds a minimal Helm chart whose helm.sh/images
// annotation lists the given image refs and saves it as a .tgz into dir.
// Returns the path to the saved .tgz file.
func seedChartWithImages(t *testing.T, dir string, images []string) string {
	t.Helper()

	// Build a helm.sh/images YAML list from the image refs.
	var sb strings.Builder
	for _, img := range images {
		sb.WriteString("- image: ")
		sb.WriteString(img)
		sb.WriteString("\n")
	}

	c := &helmchart.Chart{
		Metadata: &helmchart.Metadata{
			APIVersion: "v2",
			Name:       "test-chart",
			Version:    "0.1.0",
			Annotations: map[string]string{
				"helm.sh/images": sb.String(),
			},
		},
	}

	saved, err := chartutil.Save(c, dir)
	if err != nil {
		t.Fatalf("seedChartWithImages: chartutil.Save: %v", err)
	}
	return saved
}

// --------------------------------------------------------------------------
// ImagesFromChartAnnotations tests
// --------------------------------------------------------------------------

func TestImagesFromChartAnnotations_NilChart(t *testing.T) {
	got, err := ImagesFromChartAnnotations(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestImagesFromChartAnnotations_NoAnnotations(t *testing.T) {
	c := &helmchart.Chart{Metadata: &helmchart.Metadata{}}
	got, err := ImagesFromChartAnnotations(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestImagesFromChartAnnotations_HelmShImages(t *testing.T) {
	c := &helmchart.Chart{
		Metadata: &helmchart.Metadata{
			Annotations: map[string]string{
				"helm.sh/images": "- image: nginx:1.24\n- image: alpine:3.18\n",
			},
		},
	}
	got, err := ImagesFromChartAnnotations(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpine:3.18", "nginx:1.24"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImagesFromChartAnnotations_BothAnnotations(t *testing.T) {
	c := &helmchart.Chart{
		Metadata: &helmchart.Metadata{
			Annotations: map[string]string{
				"helm.sh/images": "- image: nginx:1.24\n- image: alpine:3.18\n",
				"images":         "- image: nginx:1.24\n- image: busybox:latest\n",
			},
		},
	}
	got, err := ImagesFromChartAnnotations(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpine:3.18", "busybox:latest", "nginx:1.24"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImagesFromChartAnnotations_MalformedYAML(t *testing.T) {
	c := &helmchart.Chart{
		Metadata: &helmchart.Metadata{
			Annotations: map[string]string{
				"helm.sh/images": "- image: [unclosed bracket",
			},
		},
	}
	_, err := ImagesFromChartAnnotations(c)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

// --------------------------------------------------------------------------
// ImagesFromImagesLock tests
// --------------------------------------------------------------------------

func TestImagesFromImagesLock_SingleFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "images.lock"), []byte("image: rancher/rancher:v2.9\nimage: nginx:1.24\n"), 0o644); err != nil {
		t.Fatalf("write images.lock: %v", err)
	}
	got, err := ImagesFromImagesLock(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"nginx:1.24", "rancher/rancher:v2.9"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImagesFromImagesLock_YAMLFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "images-lock.yaml"), []byte("image: alpine:3.18\n"), 0o644); err != nil {
		t.Fatalf("write images-lock.yaml: %v", err)
	}
	got, err := ImagesFromImagesLock(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpine:3.18"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestImagesFromImagesLock_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := ImagesFromImagesLock(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestImagesFromImagesLock_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "images.lock"), []byte("image: nginx:1.24\nimage: alpine:3.18\n"), 0o644); err != nil {
		t.Fatalf("write images.lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images-lock.yaml"), []byte("image: nginx:1.24\nimage: busybox:latest\n"), 0o644); err != nil {
		t.Fatalf("write images-lock.yaml: %v", err)
	}
	got, err := ImagesFromImagesLock(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"alpine:3.18", "busybox:latest", "nginx:1.24"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// --------------------------------------------------------------------------
// ApplyDefaultRegistry tests
// --------------------------------------------------------------------------

func TestApplyDefaultRegistry_EmptyImg(t *testing.T) {
	got, err := ApplyDefaultRegistry("", "myregistry.io")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestApplyDefaultRegistry_EmptyRegistry(t *testing.T) {
	got, err := ApplyDefaultRegistry("rancher/rancher:v2.9", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "rancher/rancher:v2.9" {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestApplyDefaultRegistry_NoRegistry(t *testing.T) {
	got, err := ApplyDefaultRegistry("rancher/rancher:v2.9", "myregistry.io")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "myregistry.io/rancher/rancher:v2.9"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyDefaultRegistry_HasRegistry(t *testing.T) {
	got, err := ApplyDefaultRegistry("ghcr.io/rancher/rancher:v2.9", "myregistry.io")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "ghcr.io/rancher/rancher:v2.9"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyDefaultRegistry_InvalidRef(t *testing.T) {
	_, err := ApplyDefaultRegistry("invalid ref with spaces", "myregistry.io")
	if err == nil {
		t.Fatal("expected error for invalid ref, got nil")
	}
}

// --------------------------------------------------------------------------
// AddChartWithOpts tests
// --------------------------------------------------------------------------

func TestAddChartWithOpts_LocalTgz(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	chartPath := filepath.Join(chartTestdataDir, "rancher-cluster-templates-0.5.2.tgz")
	if err := AddChartWithOpts(ctx, s, chartPath, ChartAddOptions{}); err != nil {
		t.Fatalf("AddChartWithOpts: %v", err)
	}
	assertArtifactInStore(t, s, "rancher-cluster-templates")
}

func TestAddChartWithOpts_FileDependency(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	chartPath := filepath.Join(chartTestdataDir, "chart-with-file-dependency-chart-1.0.0.tgz")
	if err := AddChartWithOpts(ctx, s, chartPath, ChartAddOptions{AddDependencies: true}); err != nil {
		t.Fatalf("AddChartWithOpts: %v", err)
	}
	assertArtifactInStore(t, s, "chart-with-file-dependency-chart")
}

func TestAddChartWithOpts_Rewrite(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	chartPath := filepath.Join(chartTestdataDir, "rancher-cluster-templates-0.5.2.tgz")
	if err := AddChartWithOpts(ctx, s, chartPath, ChartAddOptions{Rewrite: "myorg/custom-chart"}); err != nil {
		t.Fatalf("AddChartWithOpts with rewrite: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/custom-chart")
}

func TestAddChartWithOpts_AddImages_ExcludeExtras(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img := seedImage(t, host, "test/chart-image", "v1", rOpts...)
	seedCosignV2Artifacts(t, host, "test/chart-image", img, rOpts...)

	chartDir := t.TempDir()
	imageRef := host + "/test/chart-image:v1"
	tgzPath := seedChartWithImages(t, chartDir, []string{imageRef})

	s := newTestStore(t)
	if err := AddChartWithOpts(ctx, s, tgzPath, ChartAddOptions{
		AddImages:     true,
		ExcludeExtras: true,
	}); err != nil {
		t.Fatalf("AddChartWithOpts with ExcludeExtras: %v", err)
	}

	assertArtifactInStore(t, s, "test-chart")
	assertArtifactInStore(t, s, "test/chart-image:v1")

	// No sig / att / sbom entries must be present.
	for _, kind := range []string{consts.KindAnnotationSigs, consts.KindAnnotationAtts, consts.KindAnnotationSboms} {
		found := false
		if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
			if desc.Annotations != nil && desc.Annotations[consts.KindAnnotationName] == kind {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatalf("walk: %v", err)
		}
		if found {
			t.Errorf("unexpected artifact with kind %q found in store when ExcludeExtras=true", kind)
		}
	}
}

func TestAddChartWithOpts_AddImages_IncludeExtras(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img := seedImage(t, host, "test/chart-image", "v2", rOpts...)
	seedCosignV2Artifacts(t, host, "test/chart-image", img, rOpts...)

	chartDir := t.TempDir()
	imageRef := host + "/test/chart-image:v2"
	tgzPath := seedChartWithImages(t, chartDir, []string{imageRef})

	s := newTestStore(t)
	if err := AddChartWithOpts(ctx, s, tgzPath, ChartAddOptions{
		AddImages:     true,
		ExcludeExtras: false,
	}); err != nil {
		t.Fatalf("AddChartWithOpts without ExcludeExtras: %v", err)
	}

	assertArtifactKindInStore(t, s, "test/chart-image:v2", consts.KindAnnotationSigs)
	assertArtifactKindInStore(t, s, "test/chart-image:v2", consts.KindAnnotationAtts)
	assertArtifactKindInStore(t, s, "test/chart-image:v2", consts.KindAnnotationSboms)
}
