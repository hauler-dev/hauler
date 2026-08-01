package file

import (
	"context"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/getter"
)

type Option func(*File)

func WithClient(c *getter.Client) Option {
	return func(f *File) {
		f.client = c
	}
}

// WithContext sets the context used by compute() when fetching content, so
// cancelling ctx aborts an in-flight fetch. See File.ctx for why this is an
// option rather than a parameter on the artifacts.OCI interface.
func WithContext(ctx context.Context) Option {
	return func(f *File) {
		f.ctx = ctx
	}
}

func WithConfig(obj interface{}, mediaType string) Option {
	return func(f *File) {
		f.config = artifacts.ToConfig(obj, artifacts.WithConfigMediaType(mediaType))
	}
}

func WithAnnotations(m map[string]string) Option {
	return func(f *File) {
		f.annotations = m
	}
}
