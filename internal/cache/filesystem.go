package cache

import (
	"io"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rancherfederal/hauler/internal/layer"
)

type fs struct {
	root string
}

func NewFilesystem(root string) Cache {
	return &fs{root: root}
}

func (f *fs) Put(l v1.Layer) (v1.Layer, error) {
	digest, err := l.Digest()
	if err != nil {
		return nil, err
	}
	diffID, err := l.DiffID()
	if err != nil {
		return nil, err
	}
	return &cachedLayer{
		Layer:  l,
		root:   f.root,
		digest: digest,
		diffID: diffID,
	}, nil
}

func (f *fs) Get(h v1.Hash) (v1.Layer, error) {
	opener := f.open(h)
	l, err := layer.FromOpener(opener)
	if os.IsNotExist(err) {
		return nil, ErrLayerNotFound
	}
	return l, err
}

func (f *fs) open(h v1.Hash) layer.Opener {
	return func() (io.ReadCloser, error) {
		return os.Open(layerpath(f.root, h))
	}
}

type cachedLayer struct {
	v1.Layer

	root           string
	digest, diffID v1.Hash
}

func (l *cachedLayer) create(h v1.Hash) (io.WriteCloser, error) {
	lp := layerpath(l.root, h)
	if err := os.MkdirAll(filepath.Dir(lp), os.ModePerm); err != nil {
		return nil, err
	}
	return os.Create(lp)
}

func (l *cachedLayer) Compressed() (io.ReadCloser, error) {
	f, err := l.create(l.digest)
	if err != nil {
		return nil, nil
	}
	rc, err := l.Layer.Compressed()
	if err != nil {
		return nil, err
	}
	return &readcloser{
		t:      io.TeeReader(rc, f),
		closes: []func() error{rc.Close, f.Close},
	}, nil
}

func (l *cachedLayer) Uncompressed() (io.ReadCloser, error) {
	f, err := l.create(l.diffID)
	if err != nil {
		return nil, err
	}
	rc, err := l.Layer.Uncompressed()
	if err != nil {
		return nil, err
	}
	return &readcloser{
		t:      io.TeeReader(rc, f),
		closes: []func() error{rc.Close, f.Close},
	}, nil
}

func layerpath(root string, h v1.Hash) string {
	return filepath.Join(root, h.Algorithm, h.Hex)
}

type readcloser struct {
	t      io.Reader
	closes []func() error
}

func (rc *readcloser) Read(b []byte) (int, error) {
	return rc.t.Read(b)
}

func (rc *readcloser) Close() error {
	var err error
	for _, c := range rc.closes {
		lastErr := c()
		if err == nil {
			err = lastErr
		}
	}
	return err
}
