package directory

import (
	"context"

	"hauler.dev/go/hauler/v2/pkg/getter"
)

type Option func(*Directory)

func WithClient(c *getter.Client) Option {
	return func(d *Directory) {
		d.client = c
	}
}

func WithContext(ctx context.Context) Option {
	return func(d *Directory) {
		d.ctx = ctx
	}
}
