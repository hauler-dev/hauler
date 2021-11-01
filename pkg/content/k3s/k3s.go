package k3s

import (
	"context"

	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
)

type K3s struct {
	Files  []file.File
	Images []image.Image
}

func NewK3s(version string) (*K3s, error) {
	bom, err := newDependencies("k3s", version)
	if err != nil {
		return nil, err
	}

	var files []file.File
	for _, f := range bom.files.Spec.Files {
		fi := file.NewFile(f)
		files = append(files, fi)
	}

	var images []image.Image
	for _, i := range bom.images.Spec.Images {
		img := image.NewImage(i)
		images = append(images, img)
	}

	return &K3s{
		Files:  files,
		Images: images,
	}, nil
}

func (k *K3s) Copy(ctx context.Context, registry string) error {
	for _, f := range k.Files {
		if err := f.Copy(ctx, registry); err != nil {
			return err
		}
	}

	for _, i := range k.Images {
		if err := i.Copy(ctx, registry); err != nil {
			return err
		}
	}

	return nil
}
