package copy

import (
	"context"
	"fmt"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/oras-project/oras-go/pkg/content"
	"github.com/oras-project/oras-go/pkg/oras"
	"github.com/sirupsen/logrus"
)

type Copier struct {
	Dir       string
	fileStore *content.FileStore
	mediaOpts []string
	logger    *logrus.Entry
}

func NewCopier(dir string, media []string, fileStore *content.FileStore) *Copier {

	return &Copier{
		Dir:       dir,
		fileStore: fileStore,
		mediaOpts: media,
		logger: logrus.WithFields(logrus.Fields{
			"store": "hauler",
		}),
	}
}

func (c Copier) Get(ctx context.Context, src string) error {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resolver := docker.NewResolver(docker.ResolverOptions{PlainHTTP: true})

	// Pull file(s) from registry and save to disk
	fmt.Printf("Pulling from %s and saving to %s\n", src, c.Dir)
	desc, _, err := oras.Pull(ctx, resolver, src, c.fileStore, oras.WithAllowedMediaTypes(c.mediaOpts))

	if err != nil {
		return err
	}

	fmt.Printf("Pulled from %s with digest %s\n", src, desc.Digest)

	return nil
}
