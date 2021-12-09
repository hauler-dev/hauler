package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/consts"
)

type stager interface {
	add(ctx context.Context, oci artifact.OCI, ref name.Reference) error

	commit(ctx context.Context, b *Store) (ocispec.Descriptor, error)

	flush(ctx context.Context) error
}

var _ stager = (*layout)(nil)

func newLayout() (*layout, error) {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return nil, fmt.Errorf("mkdir temp: %w", err)
	}
	return &layout{
		root:  tmpdir,
		blobs: make(map[digest.Digest]v1.Layer),
	}, nil
}

type layout struct {
	root string

	descs []ocispec.Descriptor
	blobs map[digest.Digest]v1.Layer
}

func (l *layout) commit(ctx context.Context, b *Store) (ocispec.Descriptor, error) {
	defer l.flush(ctx)

	var g errgroup.Group
	for d, blob := range l.blobs {
		blob := blob
		d := d
		g.Go(func() error {
			rc, err := blob.Compressed()
			if err != nil {
				return fmt.Errorf("digest %s compressed layer: %w", d.Encoded(), err)
			}
			return l.writeBlob(d, rc)
		})
	}
	if err := g.Wait(); err != nil {
		return ocispec.Descriptor{}, err
	}

	idx := ocispec.Index{
		Versioned:   specs.Versioned{SchemaVersion: 2},
		Manifests:   l.descs,
		Annotations: nil,
	}

	data, err := json.Marshal(idx)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("marshal index: %w", err)
	}
	if err := os.WriteFile(l.path("index.json"), data, 0666); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("write index: %w", err)
	}

	src, err := NewOCI(l.path(""))
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("new oci, src: %w", err)
	}

	dst, err := NewOCI(b.Root)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("new oci, dst: %w", err)
	}

	if err := src.LoadIndex(); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("load source index: %w", err)
	}

	m := src.index.Manifests[0]
	ref := m.Annotations[ocispec.AnnotationRefName]

	desc, err := oras.Copy(ctx, src, ref, dst, "",
		oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("committing staging layout: %w", err)
	}

	if err := dst.SaveIndex(); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("save destination index: %w", err)
	}

	return desc, nil
}

func (l *layout) flush(ctx context.Context) error {
	return os.RemoveAll(l.path(""))
}

func (l *layout) add(ctx context.Context, oci artifact.OCI, ref name.Reference) error {
	m, err := oci.Manifest()
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	mdata, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	mdigest := digest.FromBytes(mdata)
	l.blobs[mdigest] = static.NewLayer(mdata, m.MediaType)

	cdata, err := oci.RawConfig()
	if err != nil {
		return fmt.Errorf("raw config: %w", err)
	}

	l.blobs[digest.FromBytes(cdata)] = static.NewLayer(cdata, "")

	layers, err := oci.Layers()
	if err != nil {
		return fmt.Errorf("layers: %w", err)
	}

	for _, layer := range layers {
		h, err := layer.Digest()
		if err != nil {
			return fmt.Errorf("layer digest: %w", err)
		}

		d := digest.NewDigestFromHex(h.Algorithm, h.Hex)
		l.blobs[d] = layer
	}

	mdesc := ocispec.Descriptor{
		MediaType: oci.MediaType(),
		Digest:    mdigest,
		Size:      int64(len(mdata)),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: ref.Name(),
		},

		URLs:     nil,
		Platform: nil,
	}
	l.descs = append(l.descs, mdesc)
	return nil
}

func (l *layout) writeBlob(d digest.Digest, rc io.ReadCloser) error {
	dir := l.path("blobs", d.Algorithm().String())
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return fmt.Errorf("mkdirall: %w", err)
	}

	file := filepath.Join(dir, d.Hex())
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("stat: %w", err)
	}

	w, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer w.Close()

	_, err = io.Copy(w, rc)
	return err
}

func (l *layout) path(elem ...string) string {
	complete := []string{string(l.root)}
	return filepath.Join(append(complete, elem...)...)
}
