package memory

import "github.com/rancherfederal/hauler/pkg/artifacts"

type Option func(*Memory)

func WithConfig(obj interface{}, mediaType string) Option {
	return func(m *Memory) {
		m.config = artifacts.ToConfig(obj, artifacts.WithConfigMediaType(mediaType))
	}
}

func WithAnnotations(annotations map[string]string) Option {
	return func(m *Memory) {
		m.annotations = annotations
	}
}
