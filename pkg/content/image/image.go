package image

import (
	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/rancherfederal/hauler/pkg/artifact"
)

var _ artifact.OCI = (*Image)(nil)

func (i *Image) MediaType() string {
	mt, err := i.Image.MediaType()
	if err != nil {
		return ""
	}
	return string(mt)
}

func (i *Image) RawConfig() ([]byte, error) {
	return i.RawConfigFile()
}

type Image struct {
	gv1.Image
}

func NewImage(ref string) (*Image, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, err
	}

	img, err := remote.Image(r)
	if err != nil {
		return nil, err
	}

	return &Image{
		Image: img,
	}, nil
}
