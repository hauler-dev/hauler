package oci

import (
	"context"
	"fmt"
	"os"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/deislabs/oras/pkg/content"
	"github.com/deislabs/oras/pkg/oras"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	haulerMediaType = "application/vnd.oci.image"
)

// Get wraps the oras go module to get artifacts from a registry
func Get(ctx context.Context, src string, dst string) error {

	store := content.NewFileStore(dst)
	defer store.Close()

	resolver, err := resolver()
	if err != nil {
		return err
	}

	allowedMediaTypes := []string{
		haulerMediaType,
	}

	// Pull file(s) from registry and save to disk
	fmt.Printf("pulling from %s and saving to %s\n", src, dst)
	desc, _, err := oras.Pull(ctx, resolver, src, store, oras.WithAllowedMediaTypes(allowedMediaTypes))

	if err != nil {
		return err
	}

	fmt.Printf("pulled from %s with digest %s\n", src, desc.Digest)

	return nil
}

// Put wraps the oras go module to put artifacts into a registry
func Put(ctx context.Context, src string, dst string) error {

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	resolver, err := resolver()
	if err != nil {
		return err
	}

	store := content.NewMemoryStore()

	contents := []ocispec.Descriptor{
		store.Add(src, haulerMediaType, data),
	}

	desc, err := oras.Push(ctx, resolver, dst, store, contents)
	if err != nil {
		return err
	}

	fmt.Printf("pushed %s to %s with digest: %s", src, dst, desc.Digest)

	return nil
}

func resolver() (remotes.Resolver, error) {
	resolver := docker.NewResolver(docker.ResolverOptions{PlainHTTP: true})
	return resolver, nil
}
