package store

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	goname "github.com/google/go-containerregistry/pkg/name"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// chartTestdataDir is defined in add_test.go as "../../../../testdata".

func TestExtractCmd_File(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileContent := "hello extract test"
	url := seedFileInHTTPServer(t, "extract-me.txt", fileContent)
	if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	// reference.Parse("extract-me.txt") normalises to "hauler/extract-me.txt:latest"
	// (DefaultNamespace = "hauler", DefaultTag = "latest"). ExtractCmd builds
	// repo = RepositoryStr() + ":" + Identifier() = "hauler/extract-me.txt:latest"
	// and uses strings.Contains against the stored ref — which matches exactly.
	ref := "hauler/extract-me.txt:latest"

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}

	if err := ExtractCmd(ctx, eo, s, ref); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	// The file mapper writes the layer using its AnnotationTitle ("extract-me.txt").
	outPath := filepath.Join(destDir, "extract-me.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", outPath, err)
	}
	if string(data) != fileContent {
		t.Errorf("content mismatch: got %q, want %q", string(data), fileContent)
	}
}

func TestExtractCmd_Chart(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	o := newAddChartOpts(chartTestdataDir, "")
	if err := AddChartCmd(ctx, o, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}

	// Chart stored as "hauler/rancher-cluster-templates:0.5.2".
	ref := "hauler/rancher-cluster-templates:0.5.2"

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}

	if err := ExtractCmd(ctx, eo, s, ref); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	// The chart mapper writes the chart layer as a .tgz (using AnnotationTitle,
	// or "chart.tar.gz" as fallback).
	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tgz") || strings.HasSuffix(e.Name(), ".tar.gz") {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected a .tgz or .tar.gz in destDir, got: %v", names)
	}
}

func TestExtractCmd_NotFound(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: t.TempDir(),
	}

	err := ExtractCmd(ctx, eo, s, "hauler/nonexistent:v99")
	if err == nil {
		t.Fatal("expected error for nonexistent ref, got nil")
	}
	if !strings.Contains(err.Error(), "not found in store") {
		t.Errorf("expected 'not found in store' in error, got: %v", err)
	}
}

func TestExtractCmd_OciArtifactKindImage(t *testing.T) {
	// OCI artifacts pulled from a registry via AddImage() are always labelled
	// kind=KindAnnotationImage regardless of their actual content type (file,
	// chart, etc.).  ExtractCmd must still dispatch via the manifest's
	// Config.MediaType — not the kind annotation — so extraction works correctly.
	ctx := newTestContext(t)

	// newLocalhostRegistry is required: s.AddImage uses authn.DefaultKeychain and
	// go-containerregistry auto-selects plain HTTP only for "localhost:" hosts.
	host, rOpts := newLocalhostRegistry(t)

	// Build a synthetic OCI file artifact:
	//   config.MediaType = FileLocalConfigMediaType  (triggers Files() mapper)
	//   layer.MediaType  = FileLayerMediaType
	//   layer annotation  AnnotationTitle = "oci-pulled-file.txt"
	fileContent := []byte("oci file content from registry")
	fileLayer := static.NewLayer(fileContent, gvtypes.MediaType(consts.FileLayerMediaType))
	img, err := mutate.Append(empty.Image, mutate.Addendum{
		Layer: fileLayer,
		Annotations: map[string]string{
			ocispec.AnnotationTitle: "oci-pulled-file.txt",
		},
	})
	if err != nil {
		t.Fatalf("mutate.Append: %v", err)
	}
	img = mutate.MediaType(img, gvtypes.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, gvtypes.MediaType(consts.FileLocalConfigMediaType))

	ref := host + "/oci-artifacts/myfile:v1"
	tag, err := goname.NewTag(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("goname.NewTag: %v", err)
	}
	if err := remote.Write(tag, img, rOpts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}

	// Pull into a fresh store — AddImage sets kind=KindAnnotationImage on all manifests.
	s := newTestStore(t)
	if _, err := s.AddImage(ctx, ref, "", false, "", false, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	// ExtractCmd receives the short ref (no registry prefix) as stored in AnnotationRefName.
	// reference.Parse("oci-artifacts/myfile:v1") → repo "oci-artifacts/myfile:v1" matches.
	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}
	if err := ExtractCmd(ctx, eo, s, "oci-artifacts/myfile:v1"); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	// Files() mapper uses AnnotationTitle → "oci-pulled-file.txt".
	outPath := filepath.Join(destDir, "oci-pulled-file.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", outPath, err)
	}
	if string(data) != string(fileContent) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(fileContent))
	}
}

func TestExtractCmd_OciImageIndex_NoBinFiles(t *testing.T) {
	// Regression test: extracting an OCI image index whose platform manifests
	// carry binary layers with AnnotationTitle must yield only the named binary
	// files — no sha256:<digest>.bin metadata files.
	// Before the fix, decoding the index as an ocispec.Manifest produced an
	// empty Config.MediaType, causing FromManifest to select Default() mapper
	// which wrote config blobs and child manifests as sha256:<digest>.bin.
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	buildPlatformImg := func(content []byte, title string) gcrv1.Image {
		layer := static.NewLayer(content, gvtypes.OCILayer)
		img, err := mutate.Append(empty.Image, mutate.Addendum{
			Layer: layer,
			Annotations: map[string]string{
				ocispec.AnnotationTitle: title,
			},
		})
		if err != nil {
			t.Fatalf("mutate.Append: %v", err)
		}
		img = mutate.MediaType(img, gvtypes.OCIManifestSchema1)
		img = mutate.ConfigMediaType(img, gvtypes.MediaType(ocispec.MediaTypeImageConfig))
		return img
	}

	amd64Img := buildPlatformImg([]byte("amd64 binary content"), "mybinary.linux-amd64")
	arm64Img := buildPlatformImg([]byte("arm64 binary content"), "mybinary.linux-arm64")

	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: amd64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: arm64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)

	ref := host + "/binaries/mybinary:v1"
	tag, err := goname.NewTag(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("goname.NewTag: %v", err)
	}
	if err := remote.WriteIndex(tag, idx, rOpts...); err != nil {
		t.Fatalf("remote.WriteIndex: %v", err)
	}

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, ref, "", false, "", false, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}
	if err := ExtractCmd(ctx, eo, s, "binaries/mybinary:v1"); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	// No sha256: digest-named files should be extracted
	for _, n := range names {
		if strings.HasPrefix(n, "sha256:") {
			t.Errorf("unexpected digest-named file %q extracted (all files: %v)", n, names)
		}
	}

	// Both platform binaries must be present
	for _, want := range []string{"mybinary.linux-amd64", "mybinary.linux-arm64"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected binary %q not found... got: %v", want, names)
		}
	}
}

func TestExtractCmd_NestedImageIndex_NoBinFiles(t *testing.T) {
	// Regression test: extracting a nested OCI image index (outer index whose only
	// children are inner indexes, which in turn contain the platform manifests) must
	// yield only the named binary files — no sha256:<digest>.bin metadata files.
	// firstLeafManifest must descend through the outer index into the inner index to
	// find a leaf manifest so that FromManifest selects the correct Files() mapper.
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	buildPlatformImg := func(content []byte, title string) gcrv1.Image {
		layer := static.NewLayer(content, gvtypes.OCILayer)
		img, err := mutate.Append(empty.Image, mutate.Addendum{
			Layer: layer,
			Annotations: map[string]string{
				ocispec.AnnotationTitle: title,
			},
		})
		if err != nil {
			t.Fatalf("mutate.Append: %v", err)
		}
		img = mutate.MediaType(img, gvtypes.OCIManifestSchema1)
		img = mutate.ConfigMediaType(img, gvtypes.MediaType(ocispec.MediaTypeImageConfig))
		return img
	}

	amd64Img := buildPlatformImg([]byte("amd64 binary content"), "mybinary.linux-amd64")
	arm64Img := buildPlatformImg([]byte("arm64 binary content"), "mybinary.linux-arm64")

	// Inner index contains the leaf platform manifests.
	innerIdx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: amd64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: arm64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)

	// Outer index contains only the inner index — all children are indexes.
	outerIdx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: innerIdx,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIImageIndex,
			},
		},
	)

	ref := host + "/binaries/nested:v1"
	tag, err := goname.NewTag(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("goname.NewTag: %v", err)
	}
	if err := remote.WriteIndex(tag, outerIdx, rOpts...); err != nil {
		t.Fatalf("remote.WriteIndex: %v", err)
	}

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, ref, "", false, "", false, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}
	if err := ExtractCmd(ctx, eo, s, "binaries/nested:v1"); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	// No sha256: digest-named files should be extracted.
	for _, n := range names {
		if strings.HasPrefix(n, "sha256:") {
			t.Errorf("unexpected digest-named file %q extracted (all files: %v)", n, names)
		}
	}

	// Both platform binaries must be present.
	for _, want := range []string{"mybinary.linux-amd64", "mybinary.linux-arm64"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected binary %q not found... got: %v", want, names)
		}
	}
}

func TestExtractCmd_ContainerImage_WritesTar(t *testing.T) {
	// A real container image (no AnnotationTitle on any layer) is written as a docker load compatible tar
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	layer := static.NewLayer([]byte("layer content"), gvtypes.OCILayer)
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer})
	if err != nil {
		t.Fatalf("mutate.Append: %v", err)
	}
	img = mutate.MediaType(img, gvtypes.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, gvtypes.MediaType(ocispec.MediaTypeImageConfig))

	ref := host + "/myapp/myimage:v1"
	tag, err := goname.NewTag(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("goname.NewTag: %v", err)
	}
	if err := remote.Write(tag, img, rOpts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, ref, "", false, "", false, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}
	if err := ExtractCmd(ctx, eo, s, "myapp/myimage:v1"); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	assertOnlyEntries(t, destDir, "myimage_v1.tar")
	got, tags := loadImageTar(t, filepath.Join(destDir, "myimage_v1.tar"))
	assertSameImage(t, got, img)
	if len(tags) != 1 || !strings.HasSuffix(tags[0], "/myapp/myimage:v1") {
		t.Errorf("RepoTags = %v, want one tag ending in /myapp/myimage:v1", tags)
	}
}

func TestExtractCmd_ContainerImageIndex_WritesTar(t *testing.T) {
	// A real multi-arch container image index is written as a tar holding only the selected platform
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	buildPlatformImg := func(content []byte) gcrv1.Image {
		layer := static.NewLayer(content, gvtypes.OCILayer)
		img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: layer})
		if err != nil {
			t.Fatalf("mutate.Append: %v", err)
		}
		img = mutate.MediaType(img, gvtypes.OCIManifestSchema1)
		img = mutate.ConfigMediaType(img, gvtypes.MediaType(ocispec.MediaTypeImageConfig))
		return img
	}

	amd64Img := buildPlatformImg([]byte("amd64 content"))
	arm64Img := buildPlatformImg([]byte("arm64 content"))
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: amd64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: arm64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)

	ref := host + "/myapp/multiarch:v1"
	tag, err := goname.NewTag(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("goname.NewTag: %v", err)
	}
	if err := remote.WriteIndex(tag, idx, rOpts...); err != nil {
		t.Fatalf("remote.WriteIndex: %v", err)
	}

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, ref, "", false, "", false, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	t.Run("explicit platform", func(t *testing.T) {
		destDir := t.TempDir()
		eo := &flags.ExtractOpts{StoreRootOpts: defaultRootOpts(s.Root), DestinationDir: destDir, Platform: "linux/arm64"}
		if err := ExtractCmd(ctx, eo, s, "myapp/multiarch:v1"); err != nil {
			t.Fatalf("ExtractCmd: %v", err)
		}
		assertOnlyEntries(t, destDir, "multiarch_v1.tar")
		got, _ := loadImageTar(t, filepath.Join(destDir, "multiarch_v1.tar"))
		assertSameImage(t, got, arm64Img)
	})

	t.Run("defaults to host arch", func(t *testing.T) {
		want := amd64Img
		if runtime.GOARCH == "arm64" {
			want = arm64Img
		} else if runtime.GOARCH != "amd64" {
			t.Skipf("no test platform for [%s]", runtime.GOARCH)
		}
		destDir := t.TempDir()
		eo := &flags.ExtractOpts{StoreRootOpts: defaultRootOpts(s.Root), DestinationDir: destDir}
		if err := ExtractCmd(ctx, eo, s, "myapp/multiarch:v1"); err != nil {
			t.Fatalf("ExtractCmd: %v", err)
		}
		got, _ := loadImageTar(t, filepath.Join(destDir, "multiarch_v1.tar"))
		assertSameImage(t, got, want)
	})

	t.Run("no matching platform fails", func(t *testing.T) {
		destDir := t.TempDir()
		eo := &flags.ExtractOpts{StoreRootOpts: defaultRootOpts(s.Root), DestinationDir: destDir, Platform: "linux/s390x"}
		err := ExtractCmd(ctx, eo, s, "myapp/multiarch:v1")
		if err == nil || !strings.Contains(err.Error(), "linux/amd64, linux/arm64") {
			t.Fatalf("expected an error listing available platforms, got %v", err)
		}
		assertOnlyEntries(t, destDir)
	})

	t.Run("platform without os or arch fails", func(t *testing.T) {
		for _, p := range []string{"linux", "/arm64", "linux/"} {
			destDir := t.TempDir()
			eo := &flags.ExtractOpts{StoreRootOpts: defaultRootOpts(s.Root), DestinationDir: destDir, Platform: p}
			if err := ExtractCmd(ctx, eo, s, "myapp/multiarch:v1"); err == nil || !strings.Contains(err.Error(), "expected os/arch") {
				t.Errorf("--platform [%s]: expected an os/arch error, got %v", p, err)
			}
			assertOnlyEntries(t, destDir)
		}
	})
}

func TestExtractCmd_ContainerImage_PlatformMismatchFails(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img, err := mutate.ConfigFile(empty.Image, &gcrv1.ConfigFile{OS: "linux", Architecture: "amd64"})
	if err != nil {
		t.Fatalf("mutate.ConfigFile: %v", err)
	}
	ref := host + "/myapp/amd64only:v1"
	tag, err := goname.NewTag(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("goname.NewTag: %v", err)
	}
	if err := remote.Write(tag, img, rOpts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}
	s := newTestStore(t)
	if _, err := s.AddImage(ctx, ref, "", false, "", false, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{StoreRootOpts: defaultRootOpts(s.Root), DestinationDir: destDir, Platform: "linux/arm64"}
	if err := ExtractCmd(ctx, eo, s, "myapp/amd64only:v1"); err == nil || !strings.Contains(err.Error(), "does not match --platform") {
		t.Fatalf("expected a platform mismatch error, got %v", err)
	}
	assertOnlyEntries(t, destDir)

	eo.Platform = "linux"
	if err := ExtractCmd(ctx, eo, s, "myapp/amd64only:v1"); err == nil || !strings.Contains(err.Error(), "expected os/arch") {
		t.Fatalf("expected an os/arch error, got %v", err)
	}
	assertOnlyEntries(t, destDir)

	eo.Platform = "linux/amd64"
	if err := ExtractCmd(ctx, eo, s, "myapp/amd64only:v1"); err != nil {
		t.Fatalf("ExtractCmd with matching platform: %v", err)
	}
	assertOnlyEntries(t, destDir, "amd64only_v1.tar")
}

func TestImageTarName(t *testing.T) {
	hex := strings.Repeat("a", 64)
	tests := map[string]string{
		"hauler-dev/library/busybox:stable":           "busybox_stable.tar",
		"library/busybox:latest":                      "busybox_latest.tar",
		"distroless/base@sha256:" + hex:               "base_sha256-" + hex + ".tar",
		"myorg/app:v1@sha256:" + hex:                  "app_v1.tar",
		"custom-path/nginx:1.25-alpine":               "nginx_1.25-alpine.tar",
		"hauler-dev/kubewarden/audit-scanner:v1.35.0": "audit-scanner_v1.35.0.tar",
	}
	for ref, want := range tests {
		if got := imageTarName(ref); got != want {
			t.Errorf("imageTarName(%q) = %q, want %q", ref, got, want)
		}
	}
}

// loadImageTar reads the single image and its RepoTags back from a docker load style tar
func loadImageTar(t *testing.T, p string) (gcrv1.Image, []string) {
	t.Helper()
	opener := func() (io.ReadCloser, error) { return os.Open(p) }
	m, err := tarball.LoadManifest(opener)
	if err != nil {
		t.Fatalf("tarball.LoadManifest(%s): %v", p, err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 image in %s, got %d", p, len(m))
	}
	img, err := tarball.Image(opener, nil)
	if err != nil {
		t.Fatalf("tarball.Image(%s): %v", p, err)
	}
	return img, m[0].RepoTags
}

// assertSameImage compares the config digest and each layer's uncompressed content, since the tar rewrites the manifest and gzips layers
func assertSameImage(t *testing.T, got, want gcrv1.Image) {
	t.Helper()
	gc, err := got.ConfigName()
	if err != nil {
		t.Fatalf("got.ConfigName: %v", err)
	}
	wc, err := want.ConfigName()
	if err != nil {
		t.Fatalf("want.ConfigName: %v", err)
	}
	if gc != wc {
		t.Errorf("config digest = %s, want %s", gc, wc)
	}
	gl, err := got.Layers()
	if err != nil {
		t.Fatalf("got.Layers: %v", err)
	}
	wl, err := want.Layers()
	if err != nil {
		t.Fatalf("want.Layers: %v", err)
	}
	if len(gl) != len(wl) {
		t.Fatalf("layer count = %d, want %d", len(gl), len(wl))
	}
	for i := range gl {
		rc, err := gl[i].Uncompressed()
		if err != nil {
			t.Fatalf("layer %d Uncompressed: %v", i, err)
		}
		gd, _, err := gcrv1.SHA256(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("layer %d SHA256: %v", i, err)
		}
		wd, err := wl[i].DiffID()
		if err != nil {
			t.Fatalf("layer %d DiffID: %v", i, err)
		}
		if gd != wd {
			t.Errorf("layer %d content digest = %s, want %s", i, gd, wd)
		}
	}
}

// assertOnlyEntries fails unless dir holds exactly the named entries
func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("entries in %s = %v, want %v", dir, names, want)
	}
}

func TestExtractCmd_SubstringMatch(t *testing.T) {
	// reference.Parse applies DefaultTag ("latest") when no tag is given, so
	// Parse("hauler/extract-sub.txt") and Parse("hauler/extract-sub.txt:latest")
	// produce the same repo string "hauler/extract-sub.txt:latest".
	// This means a no-tag ref substring-matches a stored "hauler/...:latest" key.
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileContent := "substring match content"
	url := seedFileInHTTPServer(t, "extract-sub.txt", fileContent)
	if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}

	// No explicit tag — Parse adds ":latest" as default, which still matches.
	if err := ExtractCmd(ctx, eo, s, "hauler/extract-sub.txt"); err != nil {
		t.Fatalf("ExtractCmd with no-tag ref: %v", err)
	}

	outPath := filepath.Join(destDir, "extract-sub.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", outPath, err)
	}
	if string(data) != fileContent {
		t.Errorf("content mismatch: got %q, want %q", string(data), fileContent)
	}
}

// newLogCaptureContext returns a context backed by a zerolog logger that writes
// JSON log lines to buf. Use buf.String() after the call to inspect log output.
func newLogCaptureContext(buf *bytes.Buffer) context.Context {
	zl := zerolog.New(buf).With().Timestamp().Logger()
	return zl.WithContext(context.Background())
}

func TestExtractCmd_CosignArtifactsProduceNoContainerImageWarning(t *testing.T) {
	// Regression test: extracting a file ref whose store also contains cosign
	// sig/att/sbom/referrer descriptors with the same repo prefix must not emit
	// the "container images cannot be extracted" warning for those cosign descriptors.
	//
	// Before the fix, cosign manifests tripped isContainerImageManifest() and caused
	// a misleading WRN line. The fix is a kind-annotation early-return in the Walk
	// callback that mirrors the pattern in copy.go:49-58.

	var logBuf bytes.Buffer
	ctx := newLogCaptureContext(&logBuf)
	s := newTestStore(t)

	// Seed a real file artifact so ExtractCmd finds something to extract.
	fileContent := "cosign-filter test file content"
	url := seedFileInHTTPServer(t, "sigtest.txt", fileContent)
	if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	// The file is stored under "hauler/sigtest.txt:latest". Inject fake cosign
	// sig/att/sbom/referrer descriptors sharing the same AnnotationRefName so the
	// Walk callback's strings.Contains(reference, repo) filter matches them too.
	baseRef := "hauler/sigtest.txt:latest"
	for _, kind := range []string{
		consts.KindAnnotationSigs,
		consts.KindAnnotationAtts,
		consts.KindAnnotationSboms,
		consts.KindAnnotationReferrers + "/sha256" + strings.Repeat("a", 64),
	} {
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     baseRef,
			consts.KindAnnotationName:     kind,
			consts.ContainerdImageNameKey: "registry.example.com/" + baseRef,
		})
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}

	if err := ExtractCmd(ctx, eo, s, baseRef); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	// The "container images cannot be extracted" warning must NOT appear for
	// the cosign descriptors — they must be silently skipped at debug level.
	if strings.Contains(logBuf.String(), "container images cannot be extracted") {
		t.Errorf("unexpected warning in log output for cosign descriptors:\n%s", logBuf.String())
	}

	// The file artifact must still be extracted to the destination directory.
	outPath := filepath.Join(destDir, "sigtest.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", outPath, err)
	}
	if string(data) != fileContent {
		t.Errorf("content mismatch: got %q, want %q", string(data), fileContent)
	}
}

// TestExtractCmd_SuffixedSigKind_Skipped confirms a subject-suffixed sig kind
// ("dev.hauler/sigs/<hex>", written for a multi-arch image's child manifest) is
// skipped the same way as the plain form. The synthetic descriptor's digest has no
// backing blob, so before the fix (an exact kind == consts.KindAnnotationSigs
// comparison that misses the suffix) ExtractCmd would try to Fetch it and fail.
func TestExtractCmd_SuffixedSigKind_Skipped(t *testing.T) {
	var logBuf bytes.Buffer
	ctx := newLogCaptureContext(&logBuf)
	s := newTestStore(t)

	fileContent := "suffixed-sig-filter test file content"
	url := seedFileInHTTPServer(t, "suffixedsig.txt", fileContent)
	if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	baseRef := "hauler/suffixedsig.txt:latest"
	seedStoreDescriptor(t, s, map[string]string{
		ocispec.AnnotationRefName:     baseRef,
		consts.KindAnnotationName:     consts.KindAnnotationSigs + "/0123abcd",
		consts.ContainerdImageNameKey: "registry.example.com/" + baseRef,
	})

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}

	if err := ExtractCmd(ctx, eo, s, baseRef); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	outPath := filepath.Join(destDir, "suffixedsig.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", outPath, err)
	}
	if string(data) != fileContent {
		t.Errorf("content mismatch: got %q, want %q", string(data), fileContent)
	}
}
