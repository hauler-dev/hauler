package getter

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/rancherfederal/hauler/pkg/artifact"
)

type file struct{}

func (f file) Name(u *url.URL) string {
	return filepath.Base(f.path(u))
}

func (f file) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	return os.Open(f.path(u))
}

func (f file) Detect(u *url.URL) bool {
	if len(f.path(u)) == 0 {
		return false
	}

	if fi, err := os.Stat(f.path(u)); err != nil {
		return false
	} else if fi.IsDir() {
		return false
	}
	return true
}

func (f file) path(u *url.URL) string {
	return filepath.Join(u.Host, u.Path)
}

func (f file) Config(u *url.URL) artifact.Config {
	c := &config{
		Reference: u.String(),
	}
	return artifact.ToConfig(c)
}
