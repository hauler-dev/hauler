package fetcher

import "context"

type fetcher struct {}

type Fetcher interface {
	Get(context.Context, string, string) error
}