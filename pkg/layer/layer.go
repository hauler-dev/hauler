package layer

import (
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

type Opener func() (io.ReadCloser, error)

func FromOpener(opener Opener, opts ...Option) (v1.Layer, error) {
	var err error

	layer := &layer{
		mediaType:   consts.UnknownLayer,
		annotations: make(map[string]string, 1),
	}

	// This package never compresses: Compressed() and Uncompressed() both
	// read from the same opener, so digest and diffID are always equal by
	// construction and share one hash computation. A future Option adding
	// real compression would break that equality and need distinct openers.
	layer.opener = opener

	for _, opt := range opts {
		opt(layer)
	}

	if layer.digest, layer.size, err = compute(layer.opener); err != nil {
		return nil, err
	}
	layer.diffID = layer.digest

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

type Option func(*layer)

func WithMediaType(mt string) Option {
	return func(l *layer) {
		l.mediaType = mt
	}
}

func WithAnnotations(annotations map[string]string) Option {
	return func(l *layer) {
		if l.annotations == nil {
			l.annotations = make(map[string]string)
		}
		l.annotations = annotations
	}
}

type layer struct {
	digest      v1.Hash
	diffID      v1.Hash
	size        int64
	opener      Opener
	mediaType   string
	annotations map[string]string
	urls        []string
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
	return l.opener()
}

func (l layer) Uncompressed() (io.ReadCloser, error) {
	return l.opener()
}

func (l layer) Size() (int64, error) {
	return l.size, nil
}

func (l layer) MediaType() (gtypes.MediaType, error) {
	return gtypes.MediaType(l.mediaType), nil
}
