package fetcher

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sirupsen/logrus"
)

type ImageFetcher struct {
	fetcher
}

func (f ImageFetcher) Get(ctx context.Context, src string, dst string) error {
	logrus.Infof("Saving remote image %s to local path %s", src, dst)

	ref, err := name.ParseReference(src)
	if err != nil {
		return err
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	// TODO
	_ = img

	//if err := tarball.MultiRefWriteToFile()

	return nil
}
