package mapper

import (
	"fmt"
	"os"

	"github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/pkg/consts"
)

type Fn func(desc v1.Descriptor) (*os.File, error)

type maps struct {
}

func NewMapper() *maps {
	return &maps{}
}

type Mapper interface{}

type image struct{}

func (i *image) mapper() map[string]Fn {
	m := make(map[string]Fn)

	manifestMapperFn := Fn(func(desc v1.Descriptor) (*os.File, error) {
		return os.Create("manifest.json")
	})

	for _, l := range []string{consts.DockerManifestSchema2, consts.OCIManifestSchema1} {
		m[l] = manifestMapperFn
	}

	layerMapperFn := Fn(func(desc v1.Descriptor) (*os.File, error) {
		n := fmt.Sprintf("%s.tar.gz", desc.Digest.String())
		return os.Create(n)
	})

	for _, l := range []string{consts.OCILayer, consts.DockerLayer} {
		m[l] = layerMapperFn
	}

	configMapperFn := Fn(func(desc v1.Descriptor) (*os.File, error) {
		return os.Create("config.json")
	})

	for _, l := range []string{consts.DockerConfigJSON} {
		m[l] = configMapperFn
	}

	return m

}

func (i *image) identify() bool {
	return false
}
