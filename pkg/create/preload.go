package create

import (
	"context"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func SavePreloadImages(ctx context.Context, images []string, archivePath string) error {
	imageRefs := make(map[name.Reference]v1.Image)

	for _, image := range images {
		ref, err := name.ParseReference(image)
		if err != nil {
			return err
		}

		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		imageRefs[ref] = img
	}

	if len(images) == 0 {
		return nil
	}

	if err := tarball.MultiRefWriteToFile(archivePath, imageRefs); err != nil {
		return err
	}
	return nil
}
