package mutate

import (
	"encoding/json"
	"errors"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	gmutate "github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"

	"github.com/rancherfederal/hauler/pkg/artifact/v1"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/types"
)

var _ v1.Oci = (*Artifact)(nil)

type Artifact struct {
	base v1.Oci
	adds []gmutate.Addendum

	computed    bool
	manifest    *gv1.Manifest
	annotations map[string]string
	mediaType   *types.MediaType
	digestMap   map[gv1.Hash]gv1.Layer
}

func MediaType(art v1.Oci, mt types.MediaType) v1.Oci {
	return &Artifact{
		base: art,
		mediaType: &mt,
	}
}

func AppendLayers(base v1.Oci, layers ...gv1.Layer) (v1.Oci, error) {
	additions := make([]gmutate.Addendum, 0, len(layers))
	for _, layer := range layers {
		additions = append(additions, gmutate.Addendum{Layer: layer})
	}
	return Append(base, additions...)
}

func Append(base v1.Oci, adds ...gmutate.Addendum) (v1.Oci, error) {
	if len(adds) == 0 {
		return base, nil
	}
	if err := validate(adds); err != nil {
		return nil, nil
	}
	return &Artifact{
		base: base,
		adds: adds,
	}, nil
}

func (a *Artifact) Layers() ([]gv1.Layer, error) {
	if err := a.compute(); err != nil {
		return nil, err
	}

	layerIDs, err := partial.FSLayers(a)
	if err != nil {
		return nil, err
	}

	ls := make([]gv1.Layer, 0, len(layerIDs))
	for _, h := range layerIDs {
		l, err := a.LayerByDigest(h)
		if err != nil {
			return nil, err
		}
		ls = append(ls, l)
	}
	return ls, nil
}

func (a *Artifact) MediaType() (types.MediaType, error) {
	if a.mediaType != nil {
		return *a.mediaType, nil
	}
	return a.base.MediaType()
}

func (a *Artifact) LayerByDigest(h gv1.Hash) (gv1.Layer, error) {
	if layer, ok := a.digestMap[h]; ok {
		return layer, nil
	}
	return a.base.LayerByDigest(h)
}

func (a *Artifact) Digest() (gv1.Hash, error) {
	if err := a.compute(); err != nil {
		return gv1.Hash{}, err
	}
	return partial.Digest(a)
}

func (a *Artifact) Manifest() (*gv1.Manifest, error) {
	if err := a.compute(); err != nil {
		return nil, err
	}
	return a.manifest, nil
}

func (a *Artifact) RawManifest() ([]byte, error) {
	if err := a.compute(); err != nil {
		return nil, err
	}
	return json.Marshal(a.manifest)
}

func (a *Artifact) compute() error {
	if a.computed {
		return nil
	}

	digestMap := make(map[gv1.Hash]gv1.Layer)

	m, err := a.base.Manifest()
	if err != nil {
		return err
	}

	manifest := m.DeepCopy()
	manifestLayers := manifest.Layers
	for _, add := range a.adds {
		if add.Layer == nil {
			continue
		}

		desc, err := partial.Descriptor(add.Layer)
		if err != nil {
			return err
		}

		if len(add.Annotations) != 0 {
			desc.Annotations = add.Annotations
		}
		if len(add.URLs) != 0 {
			desc.URLs = add.URLs
		}

		if add.MediaType != "" {
			desc.MediaType = add.MediaType
		}

		manifestLayers = append(manifestLayers, *desc)
		digestMap[desc.Digest] = add.Layer
	}

	manifest.Layers = manifestLayers

	if a.annotations != nil {
		if manifest.Annotations == nil {
			manifest.Annotations = map[string]string{}
		}

		for k, v := range a.annotations {
			manifest.Annotations[k] = v
		}
	}

	a.manifest = manifest
	a.digestMap = digestMap
	a.computed = true
	return nil
}

func validate(adds []gmutate.Addendum) error {
	for _, add := range adds {
		if add.Layer == nil {
			return errors.New("unable to add nil layer to the artifact")
		}
	}
	return nil
}
