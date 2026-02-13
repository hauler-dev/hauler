package mapper

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ccontent "github.com/containerd/containerd/content"
	"github.com/containerd/containerd/remotes"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"hauler.dev/go/hauler/pkg/content"
)

// NewMapperFileStore creates a new file store that uses mapper functions for each detected descriptor.
//
//	This extends content.OCI, and differs in that it allows much more functionality into how each descriptor is written.
func NewMapperFileStore(root string, mapper map[string]Fn) (*store, error) {
	fs, err := content.NewOCI(root)
	if err != nil {
		return nil, err
	}
	return &store{
		OCI:    fs,
		mapper: mapper,
	}, nil
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
		store:  s.OCI,
		tag:    tag,
		ref:    hash,
		mapper: s.mapper,
	}, nil
}

type store struct {
	*content.OCI
	mapper map[string]Fn
}

func (s *pusher) Push(ctx context.Context, desc ocispec.Descriptor) (ccontent.Writer, error) {
	// For manifests and indexes (which have AnnotationRefName), discard them
	// They're metadata and don't need to be extracted
	if _, ok := content.ResolveName(desc); ok {
		// Discard manifests/indexes, they're just metadata
		return content.NewIoContentWriter(&nopCloser{io.Discard}, content.WithOutputHash(desc.Digest.String())), nil
	}

	// Check if this descriptor has a mapper for its media type
	mapperFn, hasMapper := s.mapper[desc.MediaType]
	if !hasMapper {
		// No mapper for this media type, discard it (config blobs, etc.)
		return content.NewIoContentWriter(&nopCloser{io.Discard}, content.WithOutputHash(desc.Digest.String())), nil
	}

	// Get the filename from the mapper function
	filename, err := mapperFn(desc)
	if err != nil {
		return nil, err
	}

	// Get the destination directory and create the full path
	destDir := s.store.ResolvePath("")
	fullFileName := filepath.Join(destDir, filename)

	// Create the file
	f, err := os.OpenFile(fullFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("creating file %s", fullFileName))
	}

	w := content.NewIoContentWriter(f, content.WithInputHash(desc.Digest.String()), content.WithOutputHash(desc.Digest.String()))
	return w, nil
}

type nopCloser struct {
	io.Writer
}

func (*nopCloser) Close() error { return nil }

type pusher struct {
	store  *content.OCI
	tag    string
	ref    string
	mapper map[string]Fn
}
