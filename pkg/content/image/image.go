package image

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type Image struct {
	cfg v1alpha1.Image
}

func NewImage(cfg v1alpha1.Image) Image {
	return Image{
		cfg: cfg,
	}
}

func (i Image) Copy(ctx context.Context, registry string) error {
	l := log.FromContext(ctx)

	srcRef, err := name.ParseReference(i.cfg.Ref)
	if err != nil {
		return err
	}

	img, err := remote.Image(srcRef)
	if err != nil {
		return err
	}

	dstRef, err := store.RelocateReference(srcRef, registry)
	if err != nil {
		return err
	}

	l.Infof("Copying image to: '%s'", dstRef.Name())
	if err := remote.Write(dstRef, img, remote.WithContext(ctx)); err != nil {
		return err
	}

	return nil
}
