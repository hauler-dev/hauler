package layer

import (
	"errors"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"hauler.dev/go/hauler/pkg/artifacts"
)

/*
This package is _heavily_ influenced by go-containerregistry and it's cache implementation: https://github.com/google/go-containerregistry/tree/main/pkg/v1/cache
*/

type Cache interface {
	Put(v1.Layer) (v1.Layer, error)

	Get(v1.Hash) (v1.Layer, error)
}

var ErrLayerNotFound = errors.New("layer not found")

type oci struct {
	artifacts.OCI

	c Cache
}

func OCICache(o artifacts.OCI, c Cache) artifacts.OCI {
	return &oci{
		OCI: o,
		c:   c,
	}
}

func (o *oci) Layers() ([]v1.Layer, error) {
	ls, err := o.OCI.Layers()
	if err != nil {
		return nil, err
	}

	var out []v1.Layer
	for _, l := range ls {
		out = append(out, &lazyLayer{inner: l, c: o.c})
	}
	return out, nil
}

type lazyLayer struct {
	inner v1.Layer
	c     Cache
}

func (l *lazyLayer) Compressed() (io.ReadCloser, error) {
	digest, err := l.inner.Digest()
	if err != nil {
		return nil, err
	}

	layer, err := l.getOrPut(digest)
	if err != nil {
		return nil, err
	}

	return layer.Compressed()
}

func (l *lazyLayer) Uncompressed() (io.ReadCloser, error) {
	diffID, err := l.inner.DiffID()
	if err != nil {
		return nil, err
	}

	layer, err := l.getOrPut(diffID)
	if err != nil {
		return nil, err
	}

	return layer.Uncompressed()
}

func (l *lazyLayer) getOrPut(h v1.Hash) (v1.Layer, error) {
	var layer v1.Layer
	if cl, err := l.c.Get(h); err == nil {
		layer = cl

	} else if err == ErrLayerNotFound {
		rl, err := l.c.Put(l.inner)
		if err != nil {
			return nil, err
		}
		layer = rl

	} else {
		return nil, err
	}

	return layer, nil
}

func (l *lazyLayer) Size() (int64, error)                { return l.inner.Size() }
func (l *lazyLayer) DiffID() (v1.Hash, error)            { return l.inner.Digest() }
func (l *lazyLayer) Digest() (v1.Hash, error)            { return l.inner.Digest() }
func (l *lazyLayer) MediaType() (types.MediaType, error) { return l.inner.MediaType() }
