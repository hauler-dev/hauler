package image

import (
	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	v1 "github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

var _ v1.OCI = (*image)(nil)

func (i *image) MediaType() string {
	return types.DockerManifestSchema2
}

func (i *image) RawConfig() ([]byte, error) {
	return i.RawConfigFile()
}

type image struct {
	gv1.Image
}

func NewImage(ref string) (v1.OCI, error) {
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
