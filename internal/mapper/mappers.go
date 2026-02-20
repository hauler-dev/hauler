package mapper

import (
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
)

type Fn func(desc ocispec.Descriptor) (string, error)

// FromManifest will return the appropriate content store given a reference and source type adequate for storing the results on disk
func FromManifest(manifest ocispec.Manifest, root string) (content.Target, error) {
	// First, switch on config mediatype to identify known types
	switch manifest.Config.MediaType {
	case consts.DockerConfigJSON, ocispec.MediaTypeImageConfig:
		return NewMapperFileStore(root, Images())

	case consts.ChartLayerMediaType, consts.ChartConfigMediaType:
		return NewMapperFileStore(root, Chart())

	case consts.FileLocalConfigMediaType, consts.FileDirectoryConfigMediaType, consts.FileHttpConfigMediaType:
		return NewMapperFileStore(root, Files())
	}

	// For unknown config types, check if any layer has a title annotation, which indicates a file artifact
	hasFileLayer := false
	for _, layer := range manifest.Layers {
		if _, ok := layer.Annotations[ocispec.AnnotationTitle]; ok {
			hasFileLayer = true
			break
		}
	}
	if hasFileLayer {
		return NewMapperFileStore(root, Files())
	}

	// Default fallback
	return NewMapperFileStore(root, nil)
}

func Images() map[string]Fn {
	m := make(map[string]Fn)

	manifestMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		return consts.ImageManifestFile, nil
	})

	for _, l := range []string{consts.DockerManifestSchema2, consts.DockerManifestListSchema2, consts.OCIManifestSchema1} {
		m[l] = manifestMapperFn
	}

	layerMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		return fmt.Sprintf("%s.tar.gz", desc.Digest.String()), nil
	})

	for _, l := range []string{consts.OCILayer, consts.DockerLayer} {
		m[l] = layerMapperFn
	}

	configMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		return consts.ImageConfigFile, nil
	})

	for _, l := range []string{consts.DockerConfigJSON} {
		m[l] = configMapperFn
	}

	return m
}

func Chart() map[string]Fn {
	m := make(map[string]Fn)

	chartMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		f := "chart.tar.gz"
		if _, ok := desc.Annotations[ocispec.AnnotationTitle]; ok {
			f = desc.Annotations[ocispec.AnnotationTitle]
		}
		return f, nil
	})

	provMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		return "prov.json", nil
	})

	m[consts.ChartLayerMediaType] = chartMapperFn
	m[consts.ProvLayerMediaType] = provMapperFn
	return m
}

func Files() map[string]Fn {
	m := make(map[string]Fn)

	fileMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		// Use the title annotation to determine the filename
		if title, ok := desc.Annotations[ocispec.AnnotationTitle]; ok {
			return title, nil
		}
		// Fallback to digest-based filename if no title
		return fmt.Sprintf("%s.file", desc.Digest.String()), nil
	})

	// Match the media type that's actually used in the manifest
	// (set by getter.LayerFrom in pkg/getter/getter.go)
	m[consts.FileLayerMediaType] = fileMapperFn
	m[consts.OCILayer] = fileMapperFn                          // Also handle standard OCI layers that have title annotation
	m["application/vnd.oci.image.layer.v1.tar"] = fileMapperFn // And the tar variant

	return m
}
