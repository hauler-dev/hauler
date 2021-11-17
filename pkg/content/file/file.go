package file

import (
	"context"
	"os"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"

	"github.com/rancherfederal/hauler/internal/getter"
	"github.com/rancherfederal/hauler/internal/mapper"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/consts"
)

// interface guard
var _ artifact.OCI = (*file)(nil)

type file struct {
	ref         string
	client      *getter.Client
	computed    bool
	config      artifact.Config
	blob        gv1.Layer
	manifest    *gv1.Manifest
	annotations map[string]string
}

func NewFile(ref string) *file {
	// TODO: Allow user to configure this
	client := getter.NewClient(getter.ClientOptions{})

	return &file{
		client: client,
		ref:    ref,
	}
}

func (f *file) Name(ref string) string {
	return f.client.Name(ref)
}

func (f *file) MediaType() string {
	return consts.OCIManifestSchema1
}

func (f *file) RawConfig() ([]byte, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	return f.config.Raw()
}

func (f *file) Layers() ([]gv1.Layer, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	var layers []gv1.Layer
	layers = append(layers, f.blob)
	return layers, nil
}

func (f *file) Manifest() (*gv1.Manifest, error) {
	if err := f.compute(); err != nil {
		return nil, err
	}
	return f.manifest, nil
}

func (f *file) compute() error {
	if f.computed {
		return nil
	}

	ctx := context.Background()
	blob, err := f.client.LayerFrom(ctx, f.ref)
	if err != nil {
		return err
	}

	layer, err := partial.Descriptor(blob)
	if err != nil {
		return err
	}

	cfg := f.client.Config(f.ref)
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

func Mapper() map[string]mapper.Fn {
	m := make(map[string]mapper.Fn)

	blobMapperFn := mapper.Fn(func(desc ocispec.Descriptor) (*os.File, error) {
		if _, ok := desc.Annotations[ocispec.AnnotationTitle]; !ok {
			return nil, errors.Errorf("unkown file name")
		}
		return os.Create(desc.Annotations[ocispec.AnnotationTitle])
	})

	m[consts.FileLayerMediaType] = blobMapperFn
	return m
}
