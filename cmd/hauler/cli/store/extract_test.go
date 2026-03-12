package store

import (
	"os"
	"path/filepath"
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

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/consts"
)

// chartTestdataDir is defined in add_test.go as "../../../../testdata".

func TestExtractCmd_File(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	fileContent := "hello extract test"
	url := seedFileInHTTPServer(t, "extract-me.txt", fileContent)
	if err := storeFile(ctx, s, v1.File{Path: url}); err != nil {
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
	tag, err := name.NewTag(ref, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.Write(tag, img, rOpts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}

	// Pull into a fresh store — AddImage sets kind=KindAnnotationImage on all manifests.
	s := newTestStore(t)
	if err := s.AddImage(ctx, ref, "", rOpts...); err != nil {
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
	tag, err := name.NewTag(ref, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.WriteIndex(tag, idx, rOpts...); err != nil {
		t.Fatalf("remote.WriteIndex: %v", err)
	}

	s := newTestStore(t)
	if err := s.AddImage(ctx, ref, "", rOpts...); err != nil {
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
			t.Errorf("expected binary %q not found; got: %v", want, names)
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
	tag, err := name.NewTag(ref, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.WriteIndex(tag, outerIdx, rOpts...); err != nil {
		t.Fatalf("remote.WriteIndex: %v", err)
	}

	s := newTestStore(t)
	if err := s.AddImage(ctx, ref, "", rOpts...); err != nil {
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
			t.Errorf("expected binary %q not found; got: %v", want, names)
		}
	}
}

func TestExtractCmd_ContainerImage_Skipped(t *testing.T) {
	// A real container image (no AnnotationTitle on any layer) should be skipped
	// without error and without writing any files to the destination directory.
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
	tag, err := name.NewTag(ref, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.Write(tag, img, rOpts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}

	s := newTestStore(t)
	if err := s.AddImage(ctx, ref, "", rOpts...); err != nil {
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

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected no files extracted for container image, got: %v", names)
	}
}

func TestExtractCmd_ContainerImageIndex_Skipped(t *testing.T) {
	// A real multi-arch container image index (no AnnotationTitle on any layer)
	// should be skipped without error and without writing any files.
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

	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{
			Add: buildPlatformImg([]byte("amd64 content")),
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: buildPlatformImg([]byte("arm64 content")),
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)

	ref := host + "/myapp/multiarch:v1"
	tag, err := name.NewTag(ref, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if err := remote.WriteIndex(tag, idx, rOpts...); err != nil {
		t.Fatalf("remote.WriteIndex: %v", err)
	}

	s := newTestStore(t)
	if err := s.AddImage(ctx, ref, "", rOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	destDir := t.TempDir()
	eo := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(s.Root),
		DestinationDir: destDir,
	}
	if err := ExtractCmd(ctx, eo, s, "myapp/multiarch:v1"); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected no files extracted for container image index, got: %v", names)
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
	if err := storeFile(ctx, s, v1.File{Path: url}); err != nil {
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
