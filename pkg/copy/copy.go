package copy

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/oras-project/oras-go/pkg/content"
	"github.com/oras-project/oras-go/pkg/oras"
	"github.com/sirupsen/logrus"
)

type Copier struct {
	Dir       string
	fileStore content.FileStore
	mediaType string
	logger    *logrus.Entry
}

func NewCopier(path string, media string) *Copier {

	fs, err := createFileStoreIfNotExists(path)
	if err != nil {
		return nil
	}

	return &Copier{
		Dir:       path,
		fileStore: *fs,
		mediaType: media,
		logger: logrus.WithFields(logrus.Fields{
			"store": "hauler",
		}),
	}
}

func createFileStoreIfNotExists(path string) (*content.FileStore, error) {
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, err
		}
	}

	fs := content.NewFileStore(path)
	defer fs.Close()

	return fs, nil
}

func (c Copier) Get(ctx context.Context, src string) error {

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	resolver := docker.NewResolver(docker.ResolverOptions{})

	// Pull file(s) from registry and save to disk
	fmt.Printf("Pulling from %s and saving to %s...\n", src, c.Dir)
	allowedMediaTypes := []string{(c.mediaType)}
	desc, _, err := oras.Pull(ctx, resolver, src, &c.fileStore, oras.WithAllowedMediaTypes(allowedMediaTypes))

	if err != nil {
		return err
	}

	fmt.Printf("Pulled from %s with digest %s\n", src, desc.Digest)

	return nil
}
