package store

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/uuid"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type wipBlob struct {
	size int64
	path string
}

func newBlob(path string, uuid string) (wipBlob, error) {
	blobPath := filepath.Join(path, uuid)
	f, err := os.OpenFile(blobPath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return wipBlob{}, err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	return wipBlob{
		path: blobPath,
		size: 0,
	}, nil
}

func (b wipBlob) open() (*os.File, error) {
	return os.OpenFile(b.path, os.O_WRONLY, 0666)
}

func (b wipBlob) stat() (fs.FileInfo, error) {
	return os.Stat(b.path)
}

func (b wipBlob) done() {
	defer func(path string) {
		_ = os.RemoveAll(path)
	}(b.path)
	return
}

type Layout struct {
	path layout.Path

	mux *sync.RWMutex

	log log.Logger

	wipBlobs  map[string]*wipBlob
	cachePath string
	root      string
}

func NewLayout(path string) (*Layout, error) {
	lp, err := layout.FromPath(path)
	if os.IsNotExist(err) {
		if lp, err = layout.Write(path, empty.Index); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	tmpdir, err := os.MkdirTemp("", "hauler-layout")
	if err != nil {
		return nil, err
	}

	root, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	return &Layout{
		path:      lp,
		root:      root,
		mux:       &sync.RWMutex{},
		wipBlobs:  make(map[string]*wipBlob),
		cachePath: tmpdir,
	}, nil
}

func (l *Layout) GetManifest(repo string, ref string) (v1.Descriptor, io.ReadCloser, error) {
	fullRef, err := ParseRepoAndReference(repo, ref)
	if err != nil {
		return v1.Descriptor{}, nil, err
	}

	idx, _ := l.path.ImageIndex()
	idxManifest, _ := idx.IndexManifest()

	found := false
	var d v1.Descriptor
	for _, descriptor := range idxManifest.Manifests {
		if v, ok := descriptor.Annotations[AnnotationRepository]; ok {
			if v != fullRef.Context().Name() {
				continue
			}

			// Digest <-> Digest
			if descriptor.Digest.String() == fullRef.Identifier() {
				found = true
				d = descriptor
			}

			// Tag <-> Tag
			if vv, ok := descriptor.Annotations[ocispecv1.AnnotationRefName]; ok {
				if vv == fullRef.Identifier() {
					found = true
					d = descriptor
				}
			}
		}
	}

	if !found {
		// TODO: Replace with real error
		return v1.Descriptor{}, nil, fmt.Errorf("not found")
	}

	b, err := l.path.Blob(d.Digest)
	return d, b, err
}

func (l Layout) WriteManifest(content io.ReadCloser) (*v1.Manifest, error) {
	// TODO
	return v1.ParseManifest(content)
}

// NewBlobCache will create a new location based blob cache
func (l *Layout) NewBlobCache() (string, error) {
	u := uuid.New()
	if _, ok := l.wipBlobs[u.String()]; ok {
		// This should _never_ happen, but check it for completeness
		return "", fmt.Errorf("how...")
	}

	b, err := newBlob(l.cachePath, u.String())
	if err != nil {
		return "", err
	}

	l.wipBlobs[u.String()] = &b
	return u.String(), nil
}

// GetBlob returns the blobs read closer from the layout
func (l Layout) GetBlob(repo name.Reference, h v1.Hash) (io.ReadCloser, error) {
	return l.path.Blob(h)
}

// update will update the wipBlob's content and size
func (b *wipBlob) update(start int64, content io.ReadCloser) (int64, error) {
	f, err := b.open()
	if err != nil {
		return 0, err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	// Start from a strict byte
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return 0, err
	}

	written, err := io.Copy(f, content)
	if err != nil {
		return 0, err
	}

	b.size += written
	return written, nil
}

func (l Layout) StreamBlob(location string, content io.ReadCloser) (int64, error) {
	blob, err := l.getWipBlob(location)
	if err != nil {
		return 0, nil
	}

	l.mux.Lock()
	defer l.mux.Unlock()

	return blob.update(blob.size, content)
}

// UpdateBlob takes a blob stream ReadCloser and copies the content to it
// 		The content length is validated (to-from+1)
// 		The current blob size is validated against the desired from, to prevent out of order blob uploading
func (l Layout) UpdateBlob(location string, from, to int64, content io.ReadCloser) (int64, error) {
	blob, err := l.getWipBlob(location)
	if err != nil {
		return 0, err
	}

	if blob.size != from {
		return 0, fmt.Errorf("expected start of %d, got %d", blob.size, from)
	}

	l.mux.Lock()
	defer l.mux.Unlock()

	written, err := blob.update(from, content)
	if err != nil {
		return 0, err
	}

	desired := to - from + 1 // inclusive
	if written != desired {
		return 0, fmt.Errorf("written %d bytes but expected %d", written, desired)
	}

	return written, nil
}

// FinishBlob closes a blob stream, either with or without content
// 		The final contents digest is validated against the desired digest
// 		The wipBlob is written to it's final layout location
func (l Layout) FinishBlob(digest string, location string, content io.ReadCloser) error {
	h, err := v1.NewHash(digest)
	if err != nil {
		return err
	}

	l.mux.Lock()
	defer l.mux.Unlock()

	blob, err := l.getWipBlob(location)
	if err != nil {
		return fmt.Errorf("location: %s does not exist", location)
	}

	f, err := blob.open()
	if err != nil {
		return err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	if _, err := io.Copy(f, content); err != nil {
		return err
	}
	defer blob.done()

	// TODO: Validate final contents

	dir := filepath.Join(l.root, "blobs", h.Algorithm)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}

	// Move blob to it's final location
	err = moveFile(blob.path, l.blobPath(h))
	return err
}

func (l Layout) blobPath(h v1.Hash) string {
	return filepath.Join(l.root, "blobs", h.Algorithm, h.Hex)
}

// getWipBlob will return an existing blobs read closer
func (l Layout) getWipBlob(uuid string) (*wipBlob, error) {
	if _, ok := l.wipBlobs[uuid]; !ok {
		return nil, fmt.Errorf("location: %s does not exist", uuid)
	}

	return l.wipBlobs[uuid], nil
}

// moveFile moves files on all OS, without running into "invalid cross-device link"
func moveFile(source, dest string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func(src *os.File) {
		_ = src.Close()
	}(src)

	fi, err := src.Stat()
	if err != nil {
		return err
	}

	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	perm := fi.Mode() & os.ModePerm
	dst, err := os.OpenFile(dest, flag, perm)
	if err != nil {
		return err
	}
	defer func(dst *os.File) {
		_ = dst.Close()
	}(dst)

	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	err = dst.Close()
	if err != nil {
		return err
	}
	err = src.Close()
	if err != nil {
		return err
	}

	err = os.Remove(source)
	if err != nil {
		return err
	}
	return err
}
