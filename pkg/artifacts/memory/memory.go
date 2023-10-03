package memory

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/artifacts"
	"github.com/rancherfederal/hauler/pkg/consts"
)

var _ artifacts.OCI = (*Memory)(nil)

// Memory implements the OCI interface for a generic set of bytes stored in memory.
type Memory struct {
	blob        v1.Layer
	annotations map[string]string
	config      artifacts.Config
}

type defaultConfig struct {
	MediaType string `json:"mediaType,omitempty"`
}

func NewMemory(data []byte, mt string, opts ...Option) *Memory {
	blob := static.NewLayer(data, types.MediaType(mt))

	cfg := defaultConfig{MediaType: consts.MemoryConfigMediaType}
	m := &Memory{
		blob:   blob,
		config: artifacts.ToConfig(cfg),
	}

	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Memory) MediaType() string {
	return consts.OCIManifestSchema1
}

func (m *Memory) Manifest() (*v1.Manifest, error) {
	layer, err := partial.Descriptor(m.blob)
	if err != nil {
		return nil, err
	}

	cfgDesc, err := partial.Descriptor(m.config)
	if err != nil {
		return nil, err
	}

	manifest := &v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.MediaType(m.MediaType()),
		Config:        *cfgDesc,
		Layers:        []v1.Descriptor{*layer},
		Annotations:   m.annotations,
	}

	return manifest, nil
}

func (m *Memory) RawConfig() ([]byte, error) {
	if m.config == nil {
		return []byte(`{}`), nil
	}
	return m.config.Raw()
}

func (m *Memory) Layers() ([]v1.Layer, error) {
	var layers []v1.Layer
	layers = append(layers, m.blob)
	return layers, nil
}
