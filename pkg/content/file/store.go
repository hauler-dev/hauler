package file

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	orascontent "oras.land/oras-go/pkg/content"
)

type store struct {
	*orascontent.FileStore
}

func newStore(rootPath string) *store {
	fs := orascontent.NewFileStore(rootPath)

	return &store{fs}
}

// Add wraps FileStore.Add() to allow fetching remote content
func (s *store) Add(ref string) (ocispec.Descriptor, error) {
	if err := os.MkdirAll(filepath.Join(s.ResolvePath(""), "blobs"), os.ModePerm); err != nil {
		return ocispec.Descriptor{}, err
	}

	bp, err := s.writeBlob(ref)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return s.FileStore.Add(filepath.Base(ref), LayerMediaType, bp)
}

func (s *store) writeBlob(ref string) (string, error) {
	rc, err := fetch(ref)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	d := digest.FromBytes(data)

	blobPath := filepath.Join(s.ResolvePath(""), "blobs", d.String())

	if err := os.WriteFile(blobPath, data, os.ModePerm); err != nil {
		return "", err
	}

	return blobPath, nil
}

func fetch(ref string) (io.ReadCloser, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}

	var stream io.ReadCloser
	if u.Scheme == "http" || u.Scheme == "https" {
		resp, err := http.Get(u.String())
		if err != nil {
			return nil, err
		}

		// TODO: Validate with content type
		stream = resp.Body

	} else {
		// TODO: This assumes we fallback to a file type which we might not always want
		f, err := os.Open(filepath.Join(u.Host, u.Path))
		if err != nil {
			return nil, err
		}

		stream = f
	}

	return stream, nil
}
