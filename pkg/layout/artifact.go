package layout

import (
	"bytes"
	"encoding/json"
	"io"
	"os"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"

	"github.com/rancherfederal/hauler/pkg/artifact"
)

type Path struct {
	layout.Path
}

func FromPath(path string) (Path, error) {
	p, err := layout.FromPath(path)
	if os.IsNotExist(err) {
		p, err = layout.Write(path, empty.Index)
		if err != nil {
			return Path{}, err
		}
	}
	return Path{Path: p}, err
}

func (l Path) WriteOci(o artifact.OCI, name string) (ocispec.Descriptor, error) {
	layers, err := o.Layers()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// Write layers concurrently
	var g errgroup.Group
	for _, layer := range layers {
		layer := layer
		g.Go(func() error {
			return l.writeLayer(layer)
		})
	}
	if err := g.Wait(); err != nil {
		return ocispec.Descriptor{}, err
	}

	// Write the config
	cfgBlob, err := o.RawConfig()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if err = l.writeBlob(cfgBlob); err != nil {
		return ocispec.Descriptor{}, err
	}

	m, err := o.Manifest()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	manifest, err := json.Marshal(m)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if err := l.writeBlob(manifest); err != nil {
		return ocispec.Descriptor{}, err
	}

	desc := ocispec.Descriptor{
		MediaType: o.MediaType(),
		Size:      int64(len(manifest)),
		Digest:    digest.FromBytes(manifest),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: name,
		},
	}

	if err := l.appendDescriptor(desc); err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, nil
}

// writeBlob differs from layer.WriteBlob in that it requires data instead
func (l Path) writeBlob(data []byte) error {
	h, _, err := gv1.SHA256(bytes.NewReader(data))
	if err != nil {
		return err
	}

	return l.WriteBlob(h, io.NopCloser(bytes.NewReader(data)))
}

// writeLayer is a verbatim reimplementation of layout.writeLayer
func (l Path) writeLayer(layer gv1.Layer) error {
	d, err := layer.Digest()
	if err != nil {
		return err
	}

	r, err := layer.Compressed()
	if err != nil {
		return err
	}

	return l.WriteBlob(d, r)
}

// appendDescriptor is a helper that translates a ocispec.Descriptor into a gv1.Descriptor
func (l Path) appendDescriptor(desc ocispec.Descriptor) error {
	gdesc := gv1.Descriptor{
		MediaType: gtypes.MediaType(desc.MediaType),
		Size:      desc.Size,
		Digest: gv1.Hash{
			Algorithm: desc.Digest.Algorithm().String(),
			Hex:       desc.Digest.Hex(),
		},
		URLs:        desc.URLs,
		Annotations: desc.Annotations,
	}

	return l.AppendDescriptor(gdesc)
}
