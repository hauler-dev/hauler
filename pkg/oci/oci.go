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
func Get(ctx context.Context, src string, dst string, logger log.Logger) error {

	store := content.NewFileStore(dst)
	defer store.Close()

	resolver, err := resolver()
	if err != nil {
		return err
	}

	allowedMediaTypes := getAllowedMediaTypes()

	// Pull file(s) from registry and save to disk
	logger.Infof("Pulling from %s and saving to %s\n", src, dst)

	desc, _, err := oras.Pull(ctx, resolver, src, store, oras.WithAllowedMediaTypes(allowedMediaTypes))

	if err != nil {
		return err
	}

	logger.Infof("Pulled from %s with digest %s\n", src, desc.Digest)

	return nil
}

// Put wraps the oras go module to put artifacts into a registry
func Put(ctx context.Context, src string, dst string, logger log.Logger) error {

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	resolver, err := resolver()
	if err != nil {
		return err
	}

	store := content.NewMemoryStore()

	mediaType := parseFileRef(src, "")

	contents := []ocispec.Descriptor{
		store.Add(src, mediaType, data),
	}

	desc, err := oras.Push(ctx, resolver, dst, store, contents)
	if err != nil {
		return err
	}

	logger.Infof("Pushed %s to %s with digest %s\n", src, dst, desc.Digest)

	return nil
}

func resolver() (remotes.Resolver, error) {
	resolver := docker.NewResolver(docker.ResolverOptions{PlainHTTP: true})
	return resolver, nil
}

func getAllowedMediaTypes() []string {
	return []string{
		"application/vnd.oci.image",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.unknown.config.v1+json",
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
