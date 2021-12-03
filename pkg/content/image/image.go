package image

import (
	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/rancherfederal/hauler/pkg/artifact"
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
