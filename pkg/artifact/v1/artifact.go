package v1

import (
	"encoding/json"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/artifact/v1/types"
)

// Oci is a general form of v1.Image that conforms to the OCI artifacts-spec instead of the images-spec
//  At a high level, it is not constrained by an Image's config, manifests, and layer ordinality
//  This specific implementation fully encapsulates v1.Layer's within a more generic form
type Oci interface {
	Layers() ([]v1.Layer, error)

	MediaType() (gtypes.MediaType, error)

	LayerByDigest(v1.Hash) (v1.Layer, error)

	Digest() (v1.Hash, error)

	Manifest() (*v1.Manifest, error)

	RawManifest() ([]byte, error)

	ConfigName() (v1.Hash, error)

	RawConfigFile() ([]byte, error)
}

// OCICore is the bare minimum we need to represent an artifact in an OCI layout
type OCICore interface {
	MediaType() types.MediaType

	RawManifest() ([]byte, error)

	RawConfig() ([]byte, error)

	Layers() ([]v1.Layer, error)
}

type Artifact interface {
	Oci

	Config() (*Config, error)
}

type Config interface {
	Raw() ([]byte, error)
}

type core struct {
	computed bool

	manifest  *v1.Manifest
	mediaType types.MediaType
	layers    []v1.Layer
	config Config
	digestMap map[v1.Hash]v1.Layer
}

func Core(mt types.MediaType, c Config, layers []v1.Layer) (OCICore, error) {
	return &core{
		mediaType: mt,
		config: c,
		layers: layers,
	}, nil
}

func (b *core) Manifest() (*v1.Manifest, error) {
	return &v1.Manifest{
		SchemaVersion: 2,
		MediaType:     gtypes.OCIManifestSchema1,
	}, nil
}

func (b *core) MediaType() types.MediaType {
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

	manifest.Layers = manifestLayers

	b.manifest = manifest
	b.computed = true
	return nil
}
