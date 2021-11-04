package v1

import (
	"io"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/content"
)

type Getter func() (io.ReadCloser, error)

type Layer struct {
	digest      v1.Hash
	diffID      v1.Hash
	size        int64
	getter      Getter
	annotations map[string]string
}

func (l *Layer) Digest() (v1.Hash, error) {
	return l.digest, nil
}

func (l *Layer) DiffID() (v1.Hash, error) {
	return l.diffID, nil
}

func (l *Layer) Compressed() (io.ReadCloser, error) {
	return l.getter()
}

func (l *Layer) Uncompressed() (io.ReadCloser, error) {
	return l.getter()
}

func (l *Layer) Size() (int64, error) {
	return l.size, nil
}

func (l *Layer) MediaType() (types.MediaType, error) {
	return content.UnknownLayerMediaType, nil
}

func NewLayer(getter Getter) (*Layer, error) {
	rc, err := getter()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	layer := &Layer{
		annotations: make(map[string]string, 1),
		getter:      getter,
	}

	if layer.digest, layer.size, err = v1.SHA256(rc); err != nil {
		return nil, err
	}

	// No distinction between compressed/uncompressed layers
	layer.diffID = layer.digest

	return layer, nil
}
