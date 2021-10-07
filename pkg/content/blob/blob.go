package blob

import (
	"bytes"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type Layer struct {
	data []byte
}

func NewLayer(payload []byte) *Layer {
	return &Layer{data: payload}
}

func (l *Layer) Digest() (v1.Hash, error) {
	h, _, err := v1.SHA256(bytes.NewReader(l.data))
	return h, err
}

func (l *Layer) DiffID() (v1.Hash, error) {
	h, _, err := v1.SHA256(bytes.NewReader(l.data))
	return h, err
}

func (l *Layer) Compressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(l.data)), nil
}

func (l *Layer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(l.data)), nil
}

func (l *Layer) Size() (int64, error) {
	return int64(len(l.data)), nil
}

func (l *Layer) MediaType() (types.MediaType, error) {
	return "", nil
}
