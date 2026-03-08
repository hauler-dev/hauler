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
