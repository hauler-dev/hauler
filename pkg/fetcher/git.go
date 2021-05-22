package fetcher

import (
	"context"
	"github.com/sirupsen/logrus"
	"github.com/go-git/go-git/v5"
	"os"
)

type GitFetcher struct {
	fetcher
	Bare bool
}

func (f GitFetcher) Get(ctx context.Context, src string, dst string) error {
	logrus.Infof("Cloning %s to %s", src, dst)

	_, err := git.PlainCloneContext(ctx, dst, f.Bare, &git.CloneOptions{
		URL: src,
		Progress: os.Stdout,
	})
	if err != nil {
		return err
	}

	return nil
}
