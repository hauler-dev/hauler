package store

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/pkg/oras"
	"oras.land/oras-go/pkg/target"

	"github.com/rancherfederal/hauler/pkg/artifacts"
	"github.com/rancherfederal/hauler/pkg/consts"
	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/layer"
)

type Layout struct {
	*content.OCI
	Root      string
	cache     layer.Cache
	CacheRoot string
}

type Options func(*Layout)

func WithCache(c layer.Cache, cacheDir string) Options {
	return func(l *Layout) {
		l.cache = c
		l.CacheRoot = cacheDir
	}
}

func NewLayout(rootdir string, opts ...Options) (*Layout, error) {
	ociStore, err := content.NewOCI(rootdir)
	if err != nil {
		return nil, err
	}

	if err := ociStore.LoadIndex(); err != nil {
		return nil, err
	}

	l := &Layout{
		Root: rootdir,
		OCI:  ociStore,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l, nil
}

// AddOCI adds an artifacts.OCI to the store
//
//	The method to achieve this is to save artifact.OCI to a temporary directory in an OCI layout compatible form.  Once
//	saved, the entirety of the layout is copied to the store (which is just a registry).  This allows us to not only use
//	strict types to define generic content, but provides a processing pipeline suitable for extensibility.  In the
//	future we'll allow users to define their own content that must adhere either by artifact.OCI or simply an OCI layout.
func (l *Layout) AddOCI(ctx context.Context, oci artifacts.OCI, ref string) (ocispec.Descriptor, error) {
	if l.cache != nil {
		cached := layer.OCICache(oci, l.cache)
		oci = cached
	}

	// Write manifest blob
	m, err := oci.Manifest()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	mdata, err := json.Marshal(m)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if err := l.writeBlobData(mdata); err != nil {
		return ocispec.Descriptor{}, err
	}

	// Write config blob
	cdata, err := oci.RawConfig()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	static.NewLayer(cdata, "")

	if err := l.writeBlobData(cdata); err != nil {
		return ocispec.Descriptor{}, err
	}

	// write blob layers concurrently
	layers, err := oci.Layers()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	var g errgroup.Group
	for _, lyr := range layers {
		lyr := lyr
		g.Go(func() error {
			return l.writeLayer(lyr)
		})
	}
	if err := g.Wait(); err != nil {
		return ocispec.Descriptor{}, err
	}

	// Build index
	idx := ocispec.Descriptor{
		MediaType: string(m.MediaType),
		Digest:    digest.FromBytes(mdata),
		Size:      int64(len(mdata)),
		Annotations: map[string]string{
			consts.KindAnnotationName: consts.KindAnnotation,
			ocispec.AnnotationRefName: ref,
		},
		URLs:     nil,
		Platform: nil,
	}

	return idx, l.OCI.AddIndex(idx)
}

// AddOCICollection .
func (l *Layout) AddOCICollection(ctx context.Context, collection artifacts.OCICollection) ([]ocispec.Descriptor, error) {
	cnts, err := collection.Contents()
	if err != nil {
		return nil, err
	}

	var descs []ocispec.Descriptor
	for ref, oci := range cnts {
		desc, err := l.AddOCI(ctx, oci, ref)
		if err != nil {
			return nil, err
		}
		descs = append(descs, desc)
	}
	return descs, nil
}

// Flush is a fancy name for delete-all-the-things, in this case it's as trivial as deleting oci-layout content
//
//	This can be a highly destructive operation if the store's directory happens to be inline with other non-store contents
//	To reduce the blast radius and likelihood of deleting things we don't own, Flush explicitly deletes oci-layout content only
func (l *Layout) Flush(ctx context.Context) error {
	blobs := filepath.Join(l.Root, "blobs")
	if err := os.RemoveAll(blobs); err != nil {
		return err
	}

	index := filepath.Join(l.Root, "index.json")
	if err := os.RemoveAll(index); err != nil {
		return err
	}

	layout := filepath.Join(l.Root, "oci-layout")
	if err := os.RemoveAll(layout); err != nil {
		return err
	}

	return nil
}

// Copy will copy a given reference to a given target.Target
//
//	This is essentially a wrapper around oras.Copy, but locked to this content store
func (l *Layout) Copy(ctx context.Context, ref string, to target.Target, toRef string) (ocispec.Descriptor, error) {
	return oras.Copy(ctx, l.OCI, ref, to, toRef,
		oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2, consts.DockerManifestListSchema2))
}

// CopyAll performs bulk copy operations on the stores oci layout to a provided target.Target
func (l *Layout) CopyAll(ctx context.Context, to target.Target, toMapper func(string) (string, error)) ([]ocispec.Descriptor, error) {
	var descs []ocispec.Descriptor
	err := l.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		toRef := ""
		if toMapper != nil {
			tr, err := toMapper(reference)
			if err != nil {
				return err
			}
			toRef = tr
		}

		desc, err := l.Copy(ctx, reference, to, toRef)
		if err != nil {
			return err
		}

		descs = append(descs, desc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return descs, nil
}

// Identify is a helper function that will identify a human-readable content type given a descriptor
func (l *Layout) Identify(ctx context.Context, desc ocispec.Descriptor) string {
	rc, err := l.OCI.Fetch(ctx, desc)
	if err != nil {
		return ""
	}
	defer rc.Close()

	m := struct {
		Config struct {
			MediaType string `json:"mediaType"`
		} `json:"config"`
	}{}
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return ""
	}

	return m.Config.MediaType
}

func (l *Layout) writeBlobData(data []byte) error {
	blob := static.NewLayer(data, "") // NOTE: MediaType isn't actually used in the writing
	return l.writeLayer(blob)
}

func (l *Layout) writeLayer(layer v1.Layer) error {
	d, err := layer.Digest()
	if err != nil {
		return err
	}

	r, err := layer.Compressed()
	if err != nil {
		return err
	}

	dir := filepath.Join(l.Root, "blobs", d.Algorithm)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}

	blobPath := filepath.Join(dir, d.Hex)
	// Skip entirely if something exists, assume layer is present already
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}

	w, err := os.Create(blobPath)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return err
}
