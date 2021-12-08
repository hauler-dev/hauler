package image

import (
	"github.com/google/go-containerregistry/pkg/authn"
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

func NewImage(ref string, opts ...remote.Option) (*Image, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, err
	}

	defaultOpts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	opts = append(opts, defaultOpts...)

	img, err := remote.Image(r, opts...)
	if err != nil {
		return nil, err
	}

	return &Image{
		Image: img,
	}, nil
}
