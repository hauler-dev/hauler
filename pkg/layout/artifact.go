package layout

import (
	"bytes"
	"io"
	"os"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"golang.org/x/sync/errgroup"

	"github.com/rancherfederal/hauler/pkg/artifact/v1"
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

func (l Path) WriteOci(o v1.OCICore) error {
	layers, err := o.Layers()
	if err != nil {
		return err
	}

	// Write layers concurrently
	var g errgroup.Group
	for _, layer := range layers {
		g.Go(func() error {
			return l.writeLayer(layer)
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Write the config
	cfgBlob, err := o.RawConfig()
	if err != nil {
		return err
	}

	if err = l.writeBlob(cfgBlob); err != nil {
		return err
	}

	manifest, err := o.RawManifest()
	if err != nil {
		return err
	}

	return l.writeBlob(manifest)
}

// func (l Path) AppendArtifact(art v1.Oci) error {
// 	if err := l.WriteArtifact(art); err != nil {
// 		return err
// 	}
//
// 	_, err := art.MediaType()
// 	if err != nil {
// 		return err
// 	}
//
// 	d, err := art.Digest()
// 	if err != nil {
// 		return err
// 	}
//
// 	manifest, err := art.RawManifest()
// 	if err != nil {
// 		return err
// 	}
//
// 	desc := gv1.Descriptor{
// 		MediaType: "wut wut",
// 		Size:      int64(len(manifest)),
// 		Digest:    d,
// 	}
//
// 	return l.AppendDescriptor(desc)
// }
//
// func (l Path) WriteArtifact(art v1.Oci) error {
// 	layers, err := art.Layers()
// 	if err != nil {
// 		return err
// 	}
//
// 	var g errgroup.Group
// 	for _, layer := range layers {
// 		layer := layer
// 		g.Go(func() error {
// 			return l.writeLayer(layer)
// 		})
// 	}
// 	if err := g.Wait(); err != nil {
// 		return err
// 	}
//
// 	// Write the config
// 	cfgName, err := art.ConfigName()
// 	if err != nil {
// 		return err
// 	}
// 	cfgBlob, err := art.RawConfigFile()
// 	if err != nil {
// 		return err
// 	}
//
// 	if err := l.WriteBlob(cfgName, io.NopCloser(bytes.NewReader(cfgBlob))); err != nil {
// 		return err
// 	}
//
// 	// Write the artifact's manifest
// 	d, err := art.Digest()
// 	if err != nil {
// 		return err
// 	}
//
// 	m, err := art.Manifest()
// 	if err != nil {
// 		return err
// 	}
// 	manifest, err := json.Marshal(&m)
// 	if err != nil {
// 		return err
// 	}
//
// 	return l.WriteBlob(d, io.NopCloser(bytes.NewReader(manifest)))
// }

// writeBlob differs from layer.WriteBlob in that it only requires an io.ReadCloser
func (l Path) writeBlob(data []byte) error {
	h, _ , err := gv1.SHA256(bytes.NewReader(data))
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
