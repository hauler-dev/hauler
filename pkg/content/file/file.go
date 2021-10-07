package file

import (
	"context"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/content/blob"
)

type File struct {
	v1.Image
}

func NewFile(filename string) (*File, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	base := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	f, _ := mutate.Append(base, mutate.Addendum{
		Layer: blob.NewLayer(data),
	})

	return &File{
		Image: f,
	}, nil
}

func (f *File) Copy(ctx context.Context, reference name.Reference) error {
	if err := remote.Write(reference, f.Image, remote.WithContext(ctx)); err != nil {
		return err
	}

	return nil
}
