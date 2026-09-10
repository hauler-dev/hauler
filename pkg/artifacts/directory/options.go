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

// WithName overrides the derived artifact name.
func WithName(name string) Option {
	return func(d *Directory) {
		d.nameOverride = name
	}
}
