package file

import (
	"context"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/internal/getter"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/consts"
)

// interface guard
var _ artifact.OCI = (*File)(nil)

type File struct {
	Ref string

	client *getter.Client

	computed    bool
	config      artifact.Config
	blob        gv1.Layer
	manifest    *gv1.Manifest
	annotations map[string]string
}

func NewFile(ref string, opts ...Option) *File {
	client := getter.NewClient(getter.ClientOptions{})

	f := &File{
		client: client,
		Ref:    ref,
	}

	for _, opt := range opts {
		opt(f)
	}
	return f
}

func (f *File) Name(ref string) string {
	return f.client.Name(ref)
}

func (f *File) MediaType() string {
	return consts.OCIManifestSchema1
}

func (f *File) RawConfig() ([]byte, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	return f.config.Raw()
}

func (f *File) Layers() ([]gv1.Layer, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	var layers []gv1.Layer
	layers = append(layers, f.blob)
	return layers, nil
}

func (f *File) Manifest() (*gv1.Manifest, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	return f.manifest, nil
}

func (f *File) compute() error {
	if f.computed {
		return nil
	}

	ctx := context.Background()
	blob, err := f.client.LayerFrom(ctx, f.Ref)
	if err != nil {
		return err
	}

	layer, err := partial.Descriptor(blob)
	if err != nil {
		return err
	}

	cfg := f.client.Config(f.Ref)
	if cfg == nil {
		cfg = f.client.Config(f.Ref)
	}

	cfgDesc, err := partial.Descriptor(cfg)
	if err != nil {
		return err
	}

	m := &gv1.Manifest{
		SchemaVersion: 2,
		MediaType:     gtypes.MediaType(f.MediaType()),
		Config:        *cfgDesc,
		Layers:        []gv1.Descriptor{*layer},
		Annotations:   f.annotations,
	}

	f.manifest = m
	f.config = cfg
	f.blob = blob
	f.computed = true
	return nil
}
