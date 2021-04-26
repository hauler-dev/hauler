package packager

import (
	"context"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/sirupsen/logrus"
	"os"
	"strings"
)

type image struct {
	name string
}

func NewImage(name string) image {
	i := image{
		name: name,
	}
	return i
}

// Get is a thin wrapper around remote.Get that flips between bundle and tarball formats depending on dst
func (i *image) Get(ctx context.Context, dst string) (string, error) {
	logrus.Debugf("converting %s to canonical reference", i.name)
	ref, err := name.ParseReference(i.name)
	if err != nil {
		return "", err
	}

	d, err := remote.Get(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", err
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return "", err
	}

	logrus.Infof("identified image: %s", d.Ref.String())

	switch {
	case strings.HasSuffix(dst, ".tar"):
		err := i.getAutodeploy(ctx, ref, img, dst)
		if err != nil {
			return "", err
		}
	default:
		err := i.addToBundle(ctx, ref, img, dst)
		if err != nil {
			return "", err
		}
	}

	return "", nil
}

func (i *image) getAutodeploy(ctx context.Context, ref name.Reference, img v1.Image, p string) error {
	logrus.Debugf("saving %s as autoloading image to %s", ref.String(), p)

	w, err := os.Create(p)
	if err != nil {
		return err
	}
	defer w.Close()

	err = tarball.Write(ref, img, w)
	if err != nil {
		return err
	}

	return nil
}

func (i *image) addToBundle(ctx context.Context, ref name.Reference, img v1.Image, p string) error {
	logrus.Debugf("adding %s to the bundle: %s", ref.String(), p)

	return nil
}