package store

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"

	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/moby/moby/client"
)

// spoolingDaemonClient wraps a Docker client so `docker save` runs once and is
// spooled under dir, then served to every ImageSave call. Compared with the
// library's file-buffered opener, --tempdir is honored on every platform (that
// opener uses os.CreateTemp("")) and the spool is removed by Close at function
// exit rather than by GC.
//
// Use one client per image: ImageSave memoizes the first call's images and
// errors on a later call naming different ones. Each call returns a fresh
// os.Open because tarball.Image reopens the tar per layer, concurrently.
type spoolingDaemonClient struct {
	daemon.Client
	dir string

	once   sync.Once
	images []string
	path   string
	err    error
}

func newSpoolingDaemonClient(dir string) (*spoolingDaemonClient, error) {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &spoolingDaemonClient{Client: c, dir: dir}, nil
}

// ImageSave spools the daemon's tarball on first call and serves every call from that file.
func (s *spoolingDaemonClient) ImageSave(ctx context.Context, images []string, opts ...client.ImageSaveOption) (client.ImageSaveResult, error) {
	s.once.Do(func() {
		s.images = slices.Clone(images)
		s.path, s.err = s.spool(ctx, images, opts)
	})
	if s.err != nil {
		return nil, s.err
	}
	if !slices.Equal(s.images, images) {
		return nil, fmt.Errorf("spooling client holds a save of %v, got request for %v", s.images, images)
	}
	return os.Open(s.path)
}

// spool creates the temp file before asking the daemon for the export, so a bad
// --tempdir fails before a multi-GB stream starts.
func (s *spoolingDaemonClient) spool(ctx context.Context, images []string, opts []client.ImageSaveOption) (string, error) {
	f, err := os.CreateTemp(s.dir, "hauler-docker-save-*.tar")
	if err != nil {
		return "", fmt.Errorf("creating docker save spool file: %w", err)
	}
	fail := func(err error) (string, error) {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}

	rc, err := s.Client.ImageSave(ctx, images, opts...)
	if err != nil {
		return fail(err)
	}
	defer rc.Close()

	if _, err := io.Copy(f, rc); err != nil {
		return fail(fmt.Errorf("spooling docker save output: %w", err))
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("closing docker save spool file: %w", err)
	}
	return f.Name(), nil
}

// Close removes the spool file, then closes the underlying client if it can be
// closed, returning the first error. It is safe to call more than once.
func (s *spoolingDaemonClient) Close() error {
	var first error
	if s.path != "" {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			first = err
		}
		s.path = ""
	}
	if c, ok := s.Client.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
