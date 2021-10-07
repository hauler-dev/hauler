package http

import (
	"io"
	"net/http"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/content/blob"
)

type Http struct {
	v1.Image
}

func NewHttp(url string) (*Http, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	base := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	h, _ := mutate.Append(base, mutate.Addendum{
		Layer: blob.NewLayer(data),
	})

	return &Http{
		Image: h,
	}, nil
}
