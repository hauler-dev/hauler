package mapper

import (
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/target"

	"hauler.dev/go/hauler/pkg/consts"
)

type Fn func(desc ocispec.Descriptor) (string, error)

// FromManifest will return the appropriate content store given a reference and source type adequate for storing the results on disk
func FromManifest(manifest ocispec.Manifest, root string) (target.Target, error) {
	// TODO: Don't rely solely on config mediatype
	switch manifest.Config.MediaType {
	case consts.DockerConfigJSON, consts.OCIManifestSchema1:
		s := NewMapperFileStore(root, Images())
		defer s.Close()
		return s, nil

	case consts.ChartLayerMediaType, consts.ChartConfigMediaType:
		s := NewMapperFileStore(root, Chart())
		defer s.Close()
		return s, nil

	default:
		s := NewMapperFileStore(root, nil)
		defer s.Close()
		return s, nil
	}
}

func Images() map[string]Fn {
	m := make(map[string]Fn)

	manifestMapperFn := Fn(func(desc ocispec.Descriptor) (string, error) {
		return "manifest.json", nil
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
		return "config.json", nil
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
