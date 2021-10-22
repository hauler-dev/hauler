package file

import (
	"context"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/log"
)

const (
	LayerMediaType = "application/vnd.hauler.cattle.io-artifact"
)

type File struct {
	cfg   v1alpha1.Fi
	store *store
	descs []ocispec.Descriptor
}

func NewFile(cfg v1alpha1.Fi, root string) (*File, error) {
	s := newStore(root)

	var descs []ocispec.Descriptor
	for _, blob := range cfg.Blobs {
		desc, err := s.Add(blob.Ref)
		if err != nil {
			return nil, nil
		}
		descs = append(descs, desc)
	}
	defer s.Close()

	return &File{
		cfg:   cfg,
		store: s,
		descs: descs,
	}, nil
}

func (f *File) Copy(ctx context.Context, reference name.Reference) error {
	l := log.FromContext(ctx)
	_ = l

	resolver := docker.NewResolver(docker.ResolverOptions{})

	l.Infof("Copying to %s", reference.Name())
	pushedDesc, err := oras.Push(ctx, resolver, reference.Name(), f.store, f.descs)
	if err != nil {
		return err
	}

	l.Infof("Copied with descriptor: %s", pushedDesc.Digest.String())
	return nil
}
