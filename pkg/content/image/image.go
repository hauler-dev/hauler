package image

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Image struct {
	v1.Image
}

func NewImage(reference string, opts ...remote.Option) (*Image, error) {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	img, err := remote.Image(ref, opts...)
	if err != nil {
		return nil, err
	}

	return &Image{
		Image: img,
	}, nil
}

func (i *Image) Copy(ctx context.Context, reference name.Reference) error {
	l := log.FromContext(ctx)

	l.Infof("Copying to %s", reference.Name())
	if err := remote.Write(reference, i.Image, remote.WithContext(ctx)); err != nil {
		return err
	}

	return nil
}
