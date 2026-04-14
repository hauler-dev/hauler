package mapper

import (
	"strings"
	"testing"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/pkg/consts"
)

func TestFromManifest_DockerImage(t *testing.T) {
	manifest := ocispec.Manifest{
		Config: ocispec.Descriptor{
			MediaType: consts.DockerConfigJSON,
		},
	}

	target, err := FromManifest(manifest, t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target == nil {
		t.Fatal("expected non-nil Target")
	}
}

func TestFromManifest_HelmChart(t *testing.T) {
	manifest := ocispec.Manifest{
		Config: ocispec.Descriptor{
			MediaType: consts.ChartConfigMediaType,
		},
	}

	target, err := FromManifest(manifest, t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target == nil {
		t.Fatal("expected non-nil Target")
	}
}

func TestFromManifest_File(t *testing.T) {
	manifest := ocispec.Manifest{
		Config: ocispec.Descriptor{
			MediaType: consts.FileLocalConfigMediaType,
		},
	}

	target, err := FromManifest(manifest, t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target == nil {
		t.Fatal("expected non-nil Target")
	}
}

func TestFromManifest_OciImageConfigWithTitleAnnotation(t *testing.T) {
	// OCI artifacts distributed as "fake images" (e.g. rke2-binary) use the standard
	// OCI image config type but set AnnotationTitle on their layers. FromManifest must
	// dispatch to Files() (not Images()) so the title is used as the output filename.
	manifest := ocispec.Manifest{
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: consts.OCILayer,
				Annotations: map[string]string{
					ocispec.AnnotationTitle: "rke2.linux-amd64",
				},
			},
		},
	}

	target, err := FromManifest(manifest, t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	s, ok := target.(*store)
	if !ok {
		t.Fatal("expected target to be *store")
	}
	if _, exists := s.mapper[consts.OCILayer]; !exists {
		t.Fatal("expected Files() mapper (OCILayer key) for OCI image config with title annotation")
	}
}

func TestFromManifest_FileLayerFallback(t *testing.T) {
	manifest := ocispec.Manifest{
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.unknown.config.v1+json",
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: consts.FileLayerMediaType,
				Annotations: map[string]string{
					ocispec.AnnotationTitle: "somefile.txt",
				},
			},
		},
	}

	target, err := FromManifest(manifest, t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target == nil {
		t.Fatal("expected non-nil Target")
	}

	// Verify the returned store uses the Files() mapper by checking that the
	// mapper contains the FileLayerMediaType key.
	s, ok := target.(*store)
	if !ok {
		t.Fatal("expected target to be *store")
	}
	if s.mapper == nil {
		t.Fatal("expected non-nil mapper for file layer fallback")
	}
	if _, exists := s.mapper[consts.FileLayerMediaType]; !exists {
		t.Fatal("expected mapper to contain consts.FileLayerMediaType key")
	}
}

func TestFromManifest_UnknownNoTitle(t *testing.T) {
	manifest := ocispec.Manifest{
		Config: ocispec.Descriptor{
			MediaType: "application/vnd.unknown.config.v1+json",
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: "application/vnd.unknown.layer",
			},
		},
	}

	target, err := FromManifest(manifest, t.TempDir())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target == nil {
		t.Fatal("expected non-nil Target")
	}

	// Unknown artifacts must use the Default catch-all mapper so blobs are not silently discarded
	s, ok := target.(*store)
	if !ok {
		t.Fatal("expected target to be *store")
	}
	if _, exists := s.mapper[DefaultCatchAll]; !exists {
		t.Fatal("expected default catch-all mapper for unknown artifact type")
	}
}

func TestFiles_CatchAll_WithTitle(t *testing.T) {
	// OCI artifacts with custom layer media types (e.g. rke2-binary) must be
	// extracted by the Files() catch-all when they carry AnnotationTitle.
	mappers := Files()

	fn, ok := mappers[DefaultCatchAll]
	if !ok {
		t.Fatal("Files() must contain a DefaultCatchAll entry")
	}

	d := digest.Digest("sha256:" + strings.Repeat("b", 64))
	desc := ocispec.Descriptor{
		MediaType: "application/vnd.rancher.rke2.binary",
		Digest:    d,
		Annotations: map[string]string{
			ocispec.AnnotationTitle: "rke2.linux-amd64",
		},
	}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "rke2.linux-amd64" {
		t.Errorf("expected %q, got %q", "rke2.linux-amd64", result)
	}
}

func TestFiles_CatchAll_NoTitle(t *testing.T) {
	// Blobs without AnnotationTitle (e.g. config blobs) must be discarded by the
	// Files() catch-all (empty filename = discard signal for Push).
	mappers := Files()

	fn, ok := mappers[DefaultCatchAll]
	if !ok {
		t.Fatal("Files() must contain a DefaultCatchAll entry")
	}

	d := digest.Digest("sha256:" + strings.Repeat("c", 64))
	desc := ocispec.Descriptor{
		MediaType: "application/vnd.oci.image.config.v1+json",
		Digest:    d,
	}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string (discard) for config blob, got %q", result)
	}
}

func TestImages_MapperFn(t *testing.T) {
	mappers := Images()

	fn, ok := mappers[consts.DockerLayer]
	if !ok {
		t.Fatalf("expected mapper for %s", consts.DockerLayer)
	}

	d := digest.Digest("sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	desc := ocispec.Descriptor{
		Digest: d,
	}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	expected := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890.tar.gz"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestImages_ConfigMapperFn(t *testing.T) {
	mappers := Images()

	fn, ok := mappers[consts.DockerConfigJSON]
	if !ok {
		t.Fatalf("expected mapper for %s", consts.DockerConfigJSON)
	}

	desc := ocispec.Descriptor{}
	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result != consts.ImageConfigFile {
		t.Fatalf("expected %q, got %q", consts.ImageConfigFile, result)
	}
}

func TestChart_MapperFn_WithTitle(t *testing.T) {
	mappers := Chart()

	fn, ok := mappers[consts.ChartLayerMediaType]
	if !ok {
		t.Fatalf("expected mapper for %s", consts.ChartLayerMediaType)
	}

	desc := ocispec.Descriptor{
		Annotations: map[string]string{
			ocispec.AnnotationTitle: "mychart-1.0.0.tgz",
		},
	}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result != "mychart-1.0.0.tgz" {
		t.Fatalf("expected %q, got %q", "mychart-1.0.0.tgz", result)
	}
}

func TestChart_MapperFn_NoTitle(t *testing.T) {
	mappers := Chart()

	fn, ok := mappers[consts.ChartLayerMediaType]
	if !ok {
		t.Fatalf("expected mapper for %s", consts.ChartLayerMediaType)
	}

	desc := ocispec.Descriptor{}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result != "chart.tar.gz" {
		t.Fatalf("expected %q, got %q", "chart.tar.gz", result)
	}
}

func TestFiles_MapperFn_WithTitle(t *testing.T) {
	mappers := Files()

	fn, ok := mappers[consts.FileLayerMediaType]
	if !ok {
		t.Fatalf("expected mapper for %s", consts.FileLayerMediaType)
	}

	desc := ocispec.Descriptor{
		Annotations: map[string]string{
			ocispec.AnnotationTitle: "install.sh",
		},
	}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result != "install.sh" {
		t.Fatalf("expected %q, got %q", "install.sh", result)
	}
}

func TestFiles_MapperFn_NoTitle(t *testing.T) {
	mappers := Files()

	fn, ok := mappers[consts.FileLayerMediaType]
	if !ok {
		t.Fatalf("expected mapper for %s", consts.FileLayerMediaType)
	}

	d := digest.Digest("sha256:" + strings.Repeat("a", 64))
	desc := ocispec.Descriptor{
		Digest: d,
	}

	result, err := fn(desc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.HasSuffix(result, ".file") {
		t.Fatalf("expected result to end with .file, got %q", result)
	}

	expected := "sha256:" + strings.Repeat("a", 64) + ".file"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}
