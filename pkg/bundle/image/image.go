package image

import (
	"context"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/match"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
)

type Bundle struct {
	Name string `json:"name"`
	Images []string `json:"images,omitempty"`
}

func (i Bundle) layout(path string) (layout.Path, error) {
	p, err := layout.FromPath(path)
	if os.IsNotExist(err) {
		p, err = layout.Write(path, empty.Index)
		if err != nil {
			return "", err
		}
	}
	return p, nil
}

//Sync will ensure the image bundle is synchronized with the filesystem at path provided
func (i Bundle) Sync(ctx context.Context, path string) error {
	lp, err := i.layout(filepath.Join(path, "layout"))
	if err != nil {
		return err
	}

	for _, image := range i.Images {
		logrus.Infof("storing %s", image)
		ref, err := name.ParseReference(image)
		if err != nil {
			return err
		}

		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		annotations := make(map[string]string)
		annotations[ocispec.AnnotationRefName] = ref.Name()

		//TODO: Address all errors at the end?
		err = i.add(lp, img, layout.WithAnnotations(annotations))
		if err != nil {
			return err
		}
	}

	return nil
}

//Add is a wrapper around layout.Append and layout.Replace to ensure images are added to layout idempotently
func (i Bundle) add(l layout.Path, img v1.Image, options ...layout.Option) error {
	d, err := img.Digest()
	if err != nil {
		return err
	}

	m := match.Digests(d)

	return l.ReplaceImage(img, m, options...)
}

func (i Bundle) Relocate() error {
	return nil
}