package bundle

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sirupsen/logrus"
	"os"
)

const refNameAnnotation = "org.opencontainers.image.ref.name"

type LayoutStore struct {
	Dir string
	layout layout.Path
	logger *logrus.Entry
	client *Client
}

type Client struct {

}

func NewLayoutStore(path string) *LayoutStore {
	lp, err := createLayoutIfNotExists(path)
	if err != nil {
		return nil
	}

	return &LayoutStore{
		Dir:    path,
		layout: lp,
		logger: logrus.WithFields(logrus.Fields{
			"store": "hauler",
		}),
	}
}

func createLayoutIfNotExists(path string) (layout.Path, error) {
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			return "", err
		}
	}

	lp, err := layout.Write(path, empty.Index)
	if err != nil {
		return "", err
	}

	return lp, nil
}

func (l *LayoutStore) Add(ctx context.Context, imageName string) error {
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return err
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	if err := l.appendImage(img, ref); err != nil {
		return err
	}

	return nil
}

func (l *LayoutStore) Push(ctx context.Context, imageName string) error {
	ii, err := l.layout.ImageIndex()
	if err != nil {
		return err
	}

	fmt.Println(ii)
}

func (l *LayoutStore) appendImage(img v1.Image, ref name.Reference, options ...layout.Option) error {
	if err := l.layout.WriteImage(img); err != nil {
		return err
	}

	mt, err := img.MediaType()
	if err != nil {
		return err
	}

	d, err := img.Digest()
	if err != nil {
		return err
	}

	manifest, err := img.RawManifest()
	if err != nil {
		return err
	}

	annotations := map[string]string{
		refNameAnnotation: ref.Name(),
	}

	desc := v1.Descriptor{
		MediaType:   mt,
		Size:        int64(len(manifest)),
		Digest:      d,
		URLs:        nil,
		Annotations: annotations,
		Platform:    nil,
	}

	for _, opt := range options {
		if err := opt(&desc); err != nil {
			return err
		}
	}

	return l.layout.AppendDescriptor(desc)
}