package image

import (
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/rancherfederal/hauler/pkg/artifacts"
)

var _ artifacts.OCI = (*Image)(nil)

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

// Image implements the OCI interface for Image API objects. API spec information
// is stored into the Name field.
type Image struct {
	Name string
	gv1.Image
}

func NewImage(name string, opts ...remote.Option) (*Image, error) {
	r, err := gname.ParseReference(name)
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
		Name:  name,
		Image: img,
	}, nil
}

func IsMultiArchImage(name string, opts ...remote.Option) (bool, error) {
    ref, err := gname.ParseReference(name)
    if err != nil {
        return false, fmt.Errorf("parsing reference %q: %v", name, err)
    }

	defaultOpts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	opts = append(opts, defaultOpts...)

    desc, err := remote.Get(ref, opts...)
    if err != nil {
        return false, fmt.Errorf("getting image %q: %v", name, err)
    }

    _, err = desc.ImageIndex()
    if err != nil {
        // If the descriptor could not be converted to an image index, it's not a multi-arch image
        return false, nil
    }

    // If the descriptor could be converted to an image index, it's a multi-arch image
    return true, nil
}