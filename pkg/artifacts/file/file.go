package file

import (
	"context"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/getter"
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

	// ctx is used by compute() when fetching content. artifacts.OCI
	// (Layers/Manifest/RawConfig) takes no context parameter, so a
	// struct-stored ctx is the only way to thread real cancellation through
	// to the getter. Defaults to context.Background() when unset via
	// WithContext.
	ctx context.Context
}

func NewFile(path string, opts ...Option) *File {
	client := getter.NewClient(getter.ClientOptions{})

	f := &File{
		client: client,
		Path:   path,
		ctx:    context.Background(),
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

// Size returns the total compressed byte size across every layer this file
// produces (always exactly one today, see Layers), computing the content if
// needed. compute() is memoized, so an already-added File pays no extra
// fetch cost.
func (f *File) Size() (int64, error) {
	layers, err := f.Layers()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, l := range layers {
		sz, err := l.Size()
		if err != nil {
			return 0, err
		}
		total += sz
	}
	return total, nil
}

func (f *File) compute() error {
	if f.computed {
		return nil
	}

	fetch := func() (gv1.Layer, error) {
		return f.client.LayerFrom(f.ctx, f.Path)
	}

	var blob gv1.Layer
	var err error
	if cache := layerCacheFromContext(f.ctx); cache != nil {
		blob, err = cache.getOrFetch(f.Path, fetch)
	} else {
		blob, err = fetch()
	}
	if err != nil {
		return err
	}

	layer, err := partial.Descriptor(blob)
	if err != nil {
		return err
	}

	// Manually preserve the Title annotation from the layer (set by
	// getter.LayerFrom). Copy rather than mutate in place: Descriptor()
	// returns the layer's own annotations map by reference, and blob may be
	// shared across File instances with different name overrides via a
	// LayerCache (cache.go) -- mutating in place would let whichever
	// compute() runs last overwrite every other sharer's Title.
	annotations := make(map[string]string, len(layer.Annotations)+1)
	for k, v := range layer.Annotations {
		annotations[k] = v
	}
	annotations[ocispec.AnnotationTitle] = f.client.Name(f.Path)
	layer.Annotations = annotations

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
