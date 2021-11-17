package image

import (
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/internal/mapper"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/consts"
)

var _ artifact.OCI = (*image)(nil)

func (i *image) MediaType() string {
	mt, err := i.Image.MediaType()
	if err != nil {
		return ""
	}
	return string(mt)
}

func (i *image) RawConfig() ([]byte, error) {
	return i.RawConfigFile()
}

type image struct {
	gv1.Image
}

func NewImage(ref string) (*image, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, err
	}

	img, err := remote.Image(r)
	if err != nil {
		return nil, err
	}

	return &image{
		Image: img,
	}, nil
}

func Mapper() map[string]mapper.Fn {
	m := make(map[string]mapper.Fn)

	manifestMapperFn := mapper.Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		return os.Create("manifest.json")
	})

	for _, l := range []string{consts.DockerManifestSchema2, consts.OCIManifestSchema1} {
		m[l] = manifestMapperFn
	}

	layerMapperFn := mapper.Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		n := fmt.Sprintf("%s.tar.gz", desc.Digest.String())
		return os.Create(n)
	})

	for _, l := range []string{consts.OCILayer, consts.DockerLayer} {
		m[l] = layerMapperFn
	}

	configMapperFn := mapper.Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		return os.Create("config.json")
	})

	for _, l := range []string{consts.DockerConfigJSON} {
		m[l] = configMapperFn
	}

	return m
}
