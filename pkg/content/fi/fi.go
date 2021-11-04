package fi

import (
	"encoding/json"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	gmutate "github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/static"

	"github.com/rancherfederal/hauler/pkg/content/mutate"

	v12 "github.com/rancherfederal/hauler/pkg/artifact/v1"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/empty"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/types"
	"github.com/rancherfederal/hauler/pkg/content/file"
)

func NewFi(ref string) (v12.Oci, error) {
	var err error
	base := mutate.MediaType(empty.Artifact, types.UnknownManifest)

	var addendums []gmutate.Addendum

	add := gmutate.Addendum{
		Layer:       static.NewLayer([]byte("my-content-goes-here"), "wut"),
		History:     v1.History{},
		MediaType:   file.LayerMediaType,
	}

	addendums = append(addendums, add)
	base, err = mutate.Append(base, addendums...)
	if err != nil {
		return nil, err
	}

	return &fi{base: base, adds: addendums}, nil
}

var _ v12.Oci = (*fi)(nil)

type fi struct {
	base v12.Oci
	adds []gmutate.Addendum

	computed bool
	manifest *v1.Manifest
	annotations map[string]string
	mediaType *types.MediaType
	digestMap map[v1.Hash]v1.Layer
}

func (f *fi) compute() error {
	if f.computed {
		return nil
	}

	digestMap := make(map[v1.Hash]v1.Layer)

	m, err := f.base.Manifest()
	if err != nil {
		return err
	}

	manifest := m.DeepCopy()
	manifestLayers := manifest.Layers
	for _, add := range f.adds {
		if add.Layer == nil {
			continue
		}

		desc, err := partial.Descriptor(add.Layer)
		if err != nil {
			return err
		}

		manifestLayers = append(manifestLayers, *desc)
		digestMap[desc.Digest] = add.Layer
	}

	manifest.Layers = manifestLayers

	if f.annotations != nil {
		if manifest.Annotations == nil {
			manifest.Annotations = map[string]string{}
		}

		for k, v := range f.annotations {
			manifest.Annotations[k] = v
		}
	}

	f.manifest = manifest
	f.digestMap = digestMap
	f.computed = true
	return nil
}

func (f *fi) Layers() ([]v1.Layer, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}

	layerIDs, err := partial.FSLayers(f)
	if err != nil {
		return nil, err
	}

	ls := make([]v1.Layer, 0, len(layerIDs))
	for _, h := range layerIDs {
		l, err := f.LayerByDigest(h)
		if err != nil {
			return nil, err
		}
		ls = append(ls, l)
	}
	return ls, nil
}

func (f *fi) MediaType() (types.MediaType, error) {
	if f.mediaType != nil {
		return *f.mediaType, nil
	}
	return f.base.MediaType()
}

func (f *fi) LayerByDigest(h v1.Hash) (v1.Layer, error) {
	if layer, ok := f.digestMap[h]; ok {
		return layer, nil
	}
	return f.base.LayerByDigest(h)
}

// Digest returns the sha256 of this files manifest
func (f *fi) Digest() (v1.Hash, error) {
	if err := f.compute(); err != nil {
		return v1.Hash{}, err
	}
	return partial.Digest(f)
}

func (f *fi) Manifest() (*v1.Manifest, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	return f.manifest, nil
}

func (f *fi) RawManifest() ([]byte, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	return json.Marshal(f.manifest)
}