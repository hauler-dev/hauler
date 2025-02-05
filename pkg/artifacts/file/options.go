package file

import (
	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/getter"
)

type Option func(*File)

func WithClient(c *getter.Client) Option {
	return func(f *File) {
		f.client = c
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
