package image

import (
	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	v1 "github.com/rancherfederal/hauler/pkg/artifact/v1"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/types"
)

var _ v1.OCICore = (*image)(nil)

func (i *image) MediaType() types.MediaType {
	return i.MediaType()
}

func (i *image) RawConfig() ([]byte, error) {
	return i.RawConfigFile()
}

type image struct {
	gv1.Image
}

func NewImage(ref string) (v1.OCICore, error) {
	r, _ := name.ParseReference(ref)
	img, _  := remote.Image(r)

	return &image{
		Image: img,
	}, nil
}
