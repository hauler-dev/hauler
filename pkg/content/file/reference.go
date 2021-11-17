package file

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/internal/layer"
)

// Locatable represents content that can be referenced by a
type Locatable interface {
	Locate() string

	Name() string

	Annotate() map[string]string

	Open() layer.Opener
}

var (
	_ Locatable = (*localFile)(nil)
	_ Locatable = (*remoteFile)(nil)
)

func NewLocatableLocalFile(path string) (*localFile, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &localFile{
		path: abs,
	}, nil
}

type localFile struct {
	path string
}

func (l localFile) Locate() string {
	return l.path
}

func (l localFile) Name() string {
	return filepath.Base(l.path)
}

func (l localFile) Annotate() map[string]string {
	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = l.Name() // For oras FileStore to recognize
	annotations[ocispec.AnnotationSource] = l.Locate()
	return annotations
}

func (l localFile) Open() layer.Opener {
	return func() (io.ReadCloser, error) {
		return os.Open(l.Locate())
	}
}

func NewLocatableRemoteFile(ref string) (*remoteFile, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("not a valid remote file protocol (http or https)")
	}
	return &remoteFile{
		ref: ref,
	}, err
}

type remoteFile struct {
	ref  string
	name string
}

func (r remoteFile) Locate() string {
	return r.ref
}

func (r remoteFile) Name() string {
	if r.name != "" {
		return r.name
	}
	return filepath.Base(r.ref)
}

func (r remoteFile) Annotate() map[string]string {
	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = r.Name()
	annotations[ocispec.AnnotationSource] = r.Locate()
	return annotations
}

func (r remoteFile) Open() layer.Opener {
	return func() (io.ReadCloser, error) {
		resp, err := http.Get(r.ref)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
}
