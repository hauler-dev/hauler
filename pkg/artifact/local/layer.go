package local

import (
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

type Opener func() (io.ReadCloser, error)

func LayerFromOpener(opener Opener, opts ...LayerOption) (v1.Layer, error) {
	var err error

	layer := &layer{
		mediaType:   types.UnknownLayer,
		annotations: make(map[string]string, 1),
	}

	layer.uncompressedOpener = opener
	layer.compressedOpener = func() (io.ReadCloser, error) {
		rc, err := opener()
		if err != nil {
			return nil, err
		}
		// TODO: actually compress this
		return rc, nil
	}

	for _, opt := range opts {
		opt(layer)
	}

	if layer.digest, layer.size, err = compute(layer.uncompressedOpener); err != nil {
		return nil, err
	}

	if layer.diffID, _, err = compute(layer.compressedOpener); err != nil {
		return nil, err
	}

	return layer, nil
}

func compute(opener Opener) (v1.Hash, int64, error) {
	rc, err := opener()
	if err != nil {
		return v1.Hash{}, 0, err
	}
	defer rc.Close()
	return v1.SHA256(rc)
}

type LayerOption func(*layer)

func WithMediaType(mt string) LayerOption {
	return func(l *layer) {
		l.mediaType = mt
	}
}

func WithAnnotations(annotations map[string]string) LayerOption {
	return func(l *layer) {
		if l.annotations == nil {
			l.annotations = make(map[string]string)
		}
		l.annotations = annotations
	}
}

type layer struct {
	digest             v1.Hash
	diffID             v1.Hash
	size               int64
	compressedOpener   Opener
	uncompressedOpener Opener
	mediaType          string
	annotations        map[string]string
	urls               []string
}

func (l layer) Descriptor() (*v1.Descriptor, error) {
	digest, err := l.Digest()
	if err != nil {
		return nil, err
	}
	mt, err := l.MediaType()
	if err != nil {
		return nil, err
	}
	return &v1.Descriptor{
		MediaType:   mt,
		Size:        l.size,
		Digest:      digest,
		Annotations: l.annotations,
		URLs:        l.urls,

		// TODO: Allow platforms
		Platform: nil,
	}, nil
}

func (l layer) Digest() (v1.Hash, error) {
	return l.digest, nil
}

func (l layer) DiffID() (v1.Hash, error) {
	return l.diffID, nil
}

func (l layer) Compressed() (io.ReadCloser, error) {
	return l.compressedOpener()
}

func (l layer) Uncompressed() (io.ReadCloser, error) {
	return l.uncompressedOpener()
}

func (l layer) Size() (int64, error) {
	return l.size, nil
}

func (l layer) MediaType() (gtypes.MediaType, error) {
	return gtypes.MediaType(l.mediaType), nil
}
