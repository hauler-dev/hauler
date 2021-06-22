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
	"github.com/rancherfederal/hauler/pkg/log"
)

// Get wraps the oras go module to get artifacts from a registry
func Get(ctx context.Context, src string, dst string, log log.Logger) (ocispec.Descriptor, error) {

	// Create a new file store
	store := content.NewFileStore(dst)
	defer store.Close()

	// Create remote resolver
	resolver, err := resolver()

	if err != nil {
		return ocispec.Descriptor{}, err
	}

	allowedMediaTypes := getAllowedMediaTypes()

	log.Debugf("Getting allowed media types")

	// Pull file(s) from registry and save to disk
	desc, _, err := oras.Pull(ctx, resolver, src, store, oras.WithAllowedMediaTypes(allowedMediaTypes))

	log.Debugf("Pulled content %s", src)

	if err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, err
}

// Put wraps the oras go module to put artifacts into a registry
func Put(ctx context.Context, src string, dest string, log log.Logger) (ocispec.Descriptor, error) {

	// Read data from source
	data, err := os.ReadFile(src)

	log.Debugf("Reading file from %s", src)

	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// Creating remote resolver
	resolver, err := resolver()

	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// Create a new memory store
	store := content.NewMemoryStore()

	mediaType := parseFileRef(src, "")

	log.Debugf("Found media type %v", mediaType)

	contents := []ocispec.Descriptor{
		store.Add(src, mediaType, data),
	}

	// Push file(s) to destination registry
	desc, err := oras.Push(ctx, resolver, dest, store, contents)

	log.Debugf("Pushing contents to %s", dest)

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
