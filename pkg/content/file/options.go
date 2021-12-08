package file

import (
	"github.com/rancherfederal/hauler/internal/getter"
	"github.com/rancherfederal/hauler/pkg/artifact"
)

type Option func(*file)

func WithClient(c *getter.Client) Option {
	return func(f *file) {
		f.client = c
	}
}

func WithConfig(obj interface{}, mediaType string) Option {
	return func(f *file) {
		f.config = artifact.ToConfig(obj, artifact.WithConfigMediaType(mediaType))
	}
}

func WithAnnotations(m map[string]string) Option {
	return func(f *file) {
		f.annotations = m
	}
}
