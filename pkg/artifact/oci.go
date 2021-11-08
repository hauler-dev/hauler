package artifact

import (
	"bytes"
	"encoding/json"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"

	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

// OCI is the bare minimum we need to represent an artifact in an OCI layout
// Oci is a general form of v1.Image that conforms to the OCI artifacts-spec instead of the images-spec
//  At a high level, it is not constrained by an Image's config, manifests, and layer ordinality
//  This specific implementation fully encapsulates v1.Layer's within a more generic form
type OCI interface {
	MediaType() string

	RawManifest() ([]byte, error)

	RawConfig() ([]byte, error)

	Layers() ([]v1.Layer, error)
}

type core struct {
	computed bool

	manifest  *v1.Manifest
	mediaType string
	layers    []v1.Layer
	config    Config
	digestMap map[v1.Hash]v1.Layer
}

func Core(mt string, c Config, layers []v1.Layer) (OCI, error) {
	return &core{
		mediaType: mt,
		config:    c,
		layers:    layers,
	}, nil
}

func (b *core) Manifest() (*v1.Manifest, error) {
	return &v1.Manifest{
		SchemaVersion: 2,
		// MediaType:     gtypes.OCIManifestSchema1,
	}, nil
}

func (b *core) MediaType() string {
	if err := b.compute(); err != nil {
		return types.UnknownManifest
	}
	return b.mediaType
}

func (b *core) RawManifest() ([]byte, error) {
	if err := b.compute(); err != nil {
		return nil, err
	}
	return json.Marshal(b.manifest)
}

func (b *core) RawConfig() ([]byte, error) {
	if err := b.compute(); err != nil {
		return nil, err
	}
	return b.config.Raw()
}

func (b *core) ToDescriptor(data []byte) (v1.Descriptor, error) {
	h, size, err := v1.SHA256(bytes.NewBuffer(data))
	if err != nil {
		return v1.Descriptor{}, err
	}

	return v1.Descriptor{
		MediaType: "",
		Size:      size,
		Digest:    h,

		// Data:        nil,
		// URLs:        nil,
		// Annotations: nil,
		// Platform:    nil,
	}, nil
}

func (b *core) Layers() ([]v1.Layer, error) {
	if err := b.compute(); err != nil {
		return nil, err
	}
	return b.layers, nil
}

func (b *core) compute() error {
	if b.computed {
		return nil
	}

	m, err := b.Manifest()
	if err != nil {
		return err
	}

	manifest := m.DeepCopy()
	manifestLayers := manifest.Layers

	data, err := b.config.Raw()
	if err != nil {
		return err
	}

	configDesc, err := b.ToDescriptor(data)
	if err != nil {
		return err
	}

	for _, layer := range b.layers {
		if layer == nil {
			continue
		}

		desc, err := partial.Descriptor(layer)
		if err != nil {
			return err
		}

		manifestLayers = append(manifestLayers, *desc)
	}

	manifest.Config = configDesc
	manifest.Layers = manifestLayers

	b.manifest = manifest
	b.computed = true
	return nil
}
