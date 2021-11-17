package mapper

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"time"

	ccontent "github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/remotes"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"oras.land/oras-go/pkg/content"
)

func NewStore(root string, mapper map[string]Fn) *store {
	fs := content.NewFile(root)
	return &store{
		File:   fs,
		mapper: mapper,
	}
}

func (s *store) Pusher(ctx context.Context, ref string) (remotes.Pusher, error) {
	var tag, hash string
	parts := strings.SplitN(ref, "@", 2)
	if len(parts) > 0 {
		tag = parts[0]
	}
	if len(parts) > 1 {
		hash = parts[1]
	}
	return &pusher{
		tag:    tag,
		ref:    hash,
		mapper: s.mapper,
	}, nil
}

type store struct {
	*content.File
	mapper map[string]Fn
}

type pusher struct {
	tag    string
	ref    string
	mapper map[string]Fn
}

func (s *pusher) Push(ctx context.Context, desc ocispec.Descriptor) (ccontent.Writer, error) {
	now := time.Now()

	if _, ok := s.mapper[desc.MediaType]; !ok {
		return content.NewIoContentWriter(ioutil.Discard, content.WithOutputHash(desc.Digest)), nil
	}

	f, err := s.mapper[desc.MediaType](desc)
	if err != nil {
		return nil, err
	}

	return &fileWriter{
		file:     f,
		digester: digest.Canonical.Digester(),
		status: ccontent.Status{
			Ref:       f.Name(),
			Total:     desc.Size,
			StartedAt: now,
			UpdatedAt: now,
		},
		aftercommit: nil,
	}, nil
}

type fileWriter struct {
	// store       *content.File
	file        *os.File
	digester    digest.Digester
	status      ccontent.Status
	aftercommit func() error
}

// NewFileWriter will open a new file for writing, existing file will be truncated
func NewFileWriter(path string, perm os.FileMode, size int64) (*fileWriter, error) {
	now := time.Now()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return nil, err
	}

	return &fileWriter{
		file:     f,
		digester: digest.Canonical.Digester(),
		status: ccontent.Status{
			Ref:       f.Name(),
			Total:     size,
			StartedAt: now,
			UpdatedAt: now,
		},
	}, nil
}

func (w *fileWriter) Write(p []byte) (n int, err error) {
	n, err = w.file.Write(p)
	w.digester.Hash().Write(p[:n])
	w.status.Offset += int64(len(p))
	w.status.UpdatedAt = time.Now()
	return n, err
}

func (w *fileWriter) Close() error {
	if w.file == nil {
		return nil
	}
	w.file.Sync()
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *fileWriter) Digest() digest.Digest {
	return w.digester.Digest()
}

func (w *fileWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...ccontent.Opt) error {
	var base ccontent.Info
	for _, opt := range opts {
		if err := opt(&base); err != nil {
			return err
		}
	}

	if w.file == nil {
		return errors.Wrap(errdefs.ErrFailedPrecondition, "cannot commit on closed writer")
	}
	file := w.file
	w.file = nil

	if err := file.Sync(); err != nil {
		file.Close()
		return errors.Wrap(err, "sync failed")
	}

	fileInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return errors.Wrap(err, "stat failed")
	}
	if err := file.Close(); err != nil {
		return errors.Wrap(err, "failed to close file")
	}

	if size > 0 && size != fileInfo.Size() {
		return errors.Wrapf(errdefs.ErrFailedPrecondition, "unexpected commit size %d, expected %d", fileInfo.Size(), size)
	}
	if dgst := w.digester.Digest(); expected != "" && expected != dgst {
		return errors.Wrapf(errdefs.ErrFailedPrecondition, "unexpected commit digest %s, expected %s", dgst, expected)
	}

	// w.store.set(w.desc)
	if w.aftercommit != nil {
		return w.aftercommit()
	}
	return nil
}

func (w *fileWriter) Status() (ccontent.Status, error) {
	return w.status, nil
}

func (w *fileWriter) Truncate(size int64) error {
	if size != 0 {
		return content.ErrUnsupportedSize
	}
	w.status.Offset = 0
	w.digester.Hash().Reset()
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return w.file.Truncate(0)
}
