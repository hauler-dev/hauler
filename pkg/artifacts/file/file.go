package file

import (
	"context"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/getter"
)

// interface guard
var _ artifacts.OCI = (*File)(nil)

// File implements the OCI interface for File API objects. API spec information is
// stored into the Path field.
type File struct {
	Path string

	computed    bool
	client      *getter.Client
	config      artifacts.Config
	blob        gv1.Layer
	manifest    *gv1.Manifest
	annotations map[string]string
}

func NewFile(path string, opts ...Option) *File {
	client := getter.NewClient(getter.ClientOptions{})

	f := &File{
		client: client,
		Path:   path,
	}

	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Name is the name of the file's reference
func (f *File) Name(path string) string {
	return f.client.Name(path)
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

	ctx := context.TODO()
	blob, err := f.client.LayerFrom(ctx, f.Path)
	if err != nil {
		return err
	}

	layer, err := partial.Descriptor(blob)
	if err != nil {
		return err
	}

	cfg := f.client.Config(f.Path)
	if cfg == nil {
		cfg = f.client.Config(f.Path)
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
