package create

import (
	"context"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/log"
)

func SavePreloadImages(ctx context.Context, h haul.Haul, archivePath string, logger log.Logger) error {
	imageRefs := make(map[name.Reference]v1.Image)
	images := listImages(h)

	for _, image := range images {
		ref, err := name.ParseReference(image)
		if err != nil {
			return err
		}

		logger.Info().Msgf("identified %s", ref.Name())
		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		imageRefs[ref] = img
	}

	logger.Info().Msgf("saving %d images to preload as %s", len(images), archivePath)
	if err := tarball.MultiRefWriteToFile(archivePath, imageRefs); err != nil {
		return err
	}

	return nil
}

func listImages(h haul.Haul) []string {
	images := h.Spec.Driver.Images()
	images = append(images, h.Spec.PreloadImages...)
	return images
}