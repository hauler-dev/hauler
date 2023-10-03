package getter

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"

	"github.com/rancherfederal/hauler/pkg/artifacts"
	"github.com/rancherfederal/hauler/pkg/consts"
)

type directory struct {
	*File
}

func NewDirectory() *directory {
	return &directory{File: NewFile()}
}

func (d directory) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	tmpfile, err := os.CreateTemp("", "hauler")
	if err != nil {
		return nil, err
	}

	digester := digest.Canonical.Digester()
	zw := gzip.NewWriter(io.MultiWriter(tmpfile, digester.Hash()))
	defer zw.Close()

	tarDigester := digest.Canonical.Digester()
	if err := tarDir(d.path(u), d.Name(u), io.MultiWriter(zw, tarDigester.Hash()), false); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := tmpfile.Sync(); err != nil {
		return nil, err
	}

	fi, err := os.Open(tmpfile.Name())
	if err != nil {
		return nil, err
	}

	// rc := &closer{
	// 	t: io.TeeReader(tmpfile, fi),
	// 	closes: []func() error{fi.Close, tmpfile.Close, zw.Close},
	// }
	return fi, nil
}

func (d directory) Detect(u *url.URL) bool {
	if len(d.path(u)) == 0 {
		return false
	}

	fi, err := os.Stat(d.path(u))
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func (d directory) Config(u *url.URL) artifacts.Config {
	c := &directoryConfig{
		config{Reference: u.String()},
	}
	return artifacts.ToConfig(c, artifacts.WithConfigMediaType(consts.FileDirectoryConfigMediaType))
}

type directoryConfig struct {
	config `json:",inline,omitempty"`
}

func tarDir(root string, prefix string, w io.Writer, stripTimes bool) error {
	tw := tar.NewWriter(w)
	defer tw.Close()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Rename path
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.Join(prefix, name)
		name = filepath.ToSlash(name)

		// Generate header
		var link string
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return errors.Wrap(err, path)
		}
		header.Name = name
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""

		if stripTimes {
			header.ModTime = time.Time{}
			header.AccessTime = time.Time{}
			header.ChangeTime = time.Time{}
		}

		// Write file
		if err := tw.WriteHeader(header); err != nil {
			return errors.Wrap(err, "tar")
		}
		if mode.IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tw, file); err != nil {
				return errors.Wrap(err, path)
			}
		}

		return nil
	}); err != nil {
		return err
	}
	return nil
}

type closer struct {
	t      io.Reader
	closes []func() error
}

func (c *closer) Read(p []byte) (n int, err error) {
	return c.t.Read(p)
}

func (c *closer) Close() error {
	var err error
	for _, c := range c.closes {
		lastErr := c()
		if err == nil {
			err = lastErr
		}
	}
	return err
}
