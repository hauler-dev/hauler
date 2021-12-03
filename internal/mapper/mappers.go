package mapper

import (
	"fmt"
	"os"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"

	"github.com/rancherfederal/hauler/pkg/consts"
)

type Fn func(desc ocispec.Descriptor) (*os.File, error)

func Images() map[string]Fn {
	m := make(map[string]Fn)

	manifestMapperFn := Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		return os.Create("manifest.json")
	})

	for _, l := range []string{consts.DockerManifestSchema2, consts.OCIManifestSchema1} {
		m[l] = manifestMapperFn
	}

	layerMapperFn := Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		n := fmt.Sprintf("%s.tar.gz", desc.Digest.String())
		return os.Create(n)
	})

	for _, l := range []string{consts.OCILayer, consts.DockerLayer} {
		m[l] = layerMapperFn
	}

	configMapperFn := Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		return os.Create("config.json")
	})

	for _, l := range []string{consts.DockerConfigJSON} {
		m[l] = configMapperFn
	}

	return m
}

func Files() map[string]Fn {
	m := make(map[string]Fn)

	blobMapperFn := Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		fmt.Println(desc.Annotations)
		if _, ok := desc.Annotations[ocispec.AnnotationTitle]; !ok {
			return nil, errors.Errorf("unkown file name")
		}
		return os.Create(desc.Annotations[ocispec.AnnotationTitle])
	})

	m[consts.FileLayerMediaType] = blobMapperFn
	return m
}

func Chart() map[string]Fn {
	m := make(map[string]Fn)

	chartMapperFn := Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		f := "chart.tar.gz"
		if _, ok := desc.Annotations[ocispec.AnnotationTitle]; ok {
			f = desc.Annotations[ocispec.AnnotationTitle]
		}
		return os.Create(f)
	})

	provMapperFn := Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		return os.Create("prov.json")
	})

	m[consts.ChartLayerMediaType] = chartMapperFn
	m[consts.ProvLayerMediaType] = provMapperFn
	return m
}
