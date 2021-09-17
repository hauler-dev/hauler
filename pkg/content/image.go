package content

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	godigest "github.com/opencontainers/go-digest"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Image struct {
	v1.Image

	// ref
	ref name.Reference
}

func NewImage(reference string, opts ...remote.Option) *Image {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return nil
	}

	img, err := remote.Image(ref, opts...)
	if err != nil {
		return nil
	}

	return &Image{
		img,
		ref,
	}
}

// Relocate docs
func (o Image) Relocate(ctx context.Context, registry string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	l := log.FromContext(ctx)

	// TODO: Factor this out
	var stripped string
	if _, err := godigest.Parse(o.ref.Identifier()); err == nil {
		stripped = fmt.Sprintf("%s@%s", o.ref.Context().RepositoryStr(), o.ref.Identifier())
	} else {
		stripped = fmt.Sprintf("%s:%s", o.ref.Context().RepositoryStr(), o.ref.Identifier())
	}

	l.Debugf("Image reference %s stripped to %s", o.ref.Name(), stripped)

	// relocate to registry
	rRef, err := name.ParseReference(stripped, name.WithDefaultRegistry(registry))
	if err != nil {
		return err
	}

	l.Debugf("Relocating image from '%s' --> '%s'", o.ref.Name(), rRef.Name())
	return remote.Write(rRef, o.Image)
}

func (o Image) Remove(ctx context.Context, registry string) error {
	return nil
}
