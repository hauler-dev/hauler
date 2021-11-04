package store

import (
	"context"
	"fmt"
	"os"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	content2 "oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/artifact/v1"
)

func (s *Store) Add(ctx context.Context, oci v1.Oci, ref name.Reference) error {
	if err := s.precheck(); err != nil {
		return err
	}

	relocated, err := RelocateReference(ref, s.RegistryURL())
	if err != nil {
		return err
	}

	if err := remote.Write(relocated, oci, remote.WithContext(ctx)); err != nil {
		return err
	}

	// TODO: For eventual support of user defined content, ensure all content transferring is done through oci layouts
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	// defer os.RemoveAll(tmpdir)
	fmt.Println(tmpdir)

	l, err := layout.FromPath(tmpdir)
	if os.IsNotExist(err) {
		l, err = layout.Write(tmpdir, empty.Index)
		if err != nil {
			return err
		}
	} else {
		return err
	}

	err = l.AppendImage(oci)
	if err != nil {
		return err
	}

	st, err := content2.NewOCIStore(tmpdir)
	if err != nil {
		return err
	}

	var descs []ocispec.Descriptor
	for d, desc := range st.ListReferences() {
		fmt.Println(d, desc)
		descs = append(descs, desc)
	}

	resolver := docker.NewResolver(docker.ResolverOptions{})
	_, err = oras.Push(ctx, resolver, relocated.Name(), st, descs)

	return nil
}
