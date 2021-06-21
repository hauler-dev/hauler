package oci

import (
	"context"
	"os"
	"strings"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/deislabs/oras/pkg/content"
	"github.com/deislabs/oras/pkg/oras"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Get wraps the oras go module to get artifacts from a registry
func Get(ctx context.Context, src string, dst string) (ocispec.Descriptor, error) {

	store := content.NewFileStore(dst)
	defer store.Close()

	resolver, err := resolver()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	allowedMediaTypes := getAllowedMediaTypes()

	// Pull file(s) from registry and save to disk
	desc, _, err := oras.Pull(ctx, resolver, src, store, oras.WithAllowedMediaTypes(allowedMediaTypes))

	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, err
}

// Put wraps the oras go module to put artifacts into a registry
func Put(ctx context.Context, src string, dest string) (ocispec.Descriptor, error) {

	data, err := os.ReadFile(src)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	resolver, err := resolver()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	store := content.NewMemoryStore()

	mediaType := parseFileRef(src, "")

	contents := []ocispec.Descriptor{
		store.Add(src, mediaType, data),
	}

	// Push file(s) to destination registry
	desc, err := oras.Push(ctx, resolver, dest, store, contents)

	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, err
}

func resolver() (remotes.Resolver, error) {
	resolver := docker.NewResolver(docker.ResolverOptions{PlainHTTP: true})
	return resolver, nil
}

func getAllowedMediaTypes() []string {
	return []string{
		"application/vnd.oci.image",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.layer.v1.tar",
	}
}

func parseFileRef(ref string, mediaType string) string {
	i := strings.LastIndex(ref, ":")
	if i < 0 {
		return mediaType
	}
	return ref[i+1:]
}
