package fetcher

import "context"

type GitFetcher struct {
	fetcher
}

func (f GitFetcher) Get(ctx context.Context, src string, dst string) error {
	// TODO

	return nil
}
