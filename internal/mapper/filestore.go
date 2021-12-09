package mapper

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	ccontent "github.com/containerd/containerd/content"
	"github.com/containerd/containerd/remotes"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
)

// NewMapperFileStore creates a new file store that uses mapper functions for each detected descriptor.
// 		This extends content.File, and differs in that it allows much more functionality into how each descriptor is written.
func NewMapperFileStore(root string, mapper map[string]Fn) *store {
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
		store:  s.File,
		tag:    tag,
		ref:    hash,
		mapper: s.mapper,
	}, nil
}

type store struct {
	*content.File
	mapper map[string]Fn
}

func (s *pusher) Push(ctx context.Context, desc ocispec.Descriptor) (ccontent.Writer, error) {
	// TODO: This is suuuuuper ugly... redo this when oras v2 is out
	if _, ok := content.ResolveName(desc); ok {
		p, err := s.store.Pusher(ctx, s.ref)
		if err != nil {
			return nil, err
		}
		return p.Push(ctx, desc)
	}

	// If no custom mapper found, fall back to content.File mapper
	if _, ok := s.mapper[desc.MediaType]; !ok {
		return content.NewIoContentWriter(ioutil.Discard, content.WithOutputHash(desc.Digest)), nil
	}

	filename, err := s.mapper[desc.MediaType](desc)
	if err != nil {
		return nil, err
	}

	fullFileName := filepath.Join(s.store.ResolvePath(""), filename)
	// TODO: Don't rewrite everytime, we can check the digest
	f, err := os.OpenFile(fullFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("push file: %w", err)
	}

	w := content.NewIoContentWriter(f, content.WithInputHash(desc.Digest), content.WithOutputHash(desc.Digest))
	return w, nil
}

type pusher struct {
	store  *content.File
	tag    string
	ref    string
	mapper map[string]Fn
}
