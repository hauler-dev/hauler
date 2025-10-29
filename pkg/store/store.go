package store

import (
	"context"
	"encoding/json"
	"fmt"
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

	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/layer"
)

type Layout struct {
	*content.OCI
	Root  string
	cache layer.Cache
}

type Options func(*Layout)

func WithCache(c layer.Cache) Options {
	return func(l *Layout) {
		l.cache = c
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
			consts.KindAnnotationName: consts.KindAnnotationImage,
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
	blobs := filepath.Join(l.Root, ocispec.ImageBlobsDir)
	if err := os.RemoveAll(blobs); err != nil {
		return err
	}

	index := filepath.Join(l.Root, ocispec.ImageIndexFile)
	if err := os.RemoveAll(index); err != nil {
		return err
	}

	layout := filepath.Join(l.Root, ocispec.ImageLayoutFile)
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

	dir := filepath.Join(l.Root, ocispec.ImageBlobsDir, d.Algorithm)
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

// Delete artifact reference from the store
func (l *Layout) DeleteArtifact(ctx context.Context, reference string, desc ocispec.Descriptor) error {
	if err := l.OCI.LoadIndex(); err != nil {
		return err
	}

	l.OCI.RemoveFromIndex(reference)
	return l.OCI.SaveIndex()
}

func (l *Layout) CleanUp(ctx context.Context) (int, int64, error) {
	referencedDigests := make(map[string]bool)

	if err := l.OCI.LoadIndex(); err != nil {
		return 0, 0, fmt.Errorf("failed to load index: %w", err)
	}

	// walk through remaining artifacts and collect digests
	if err := l.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		if desc.Digest.Validate() != nil {
			return nil
		}

		// mark digest as referenced by existing artifact
		referencedDigests[desc.Digest.Hex()] = true

		// fetch and parse manifests for layer digests
		rc, err := l.OCI.Fetch(ctx, desc)
		if err != nil {
			return nil // skip if can't be read
		}
		defer rc.Close()

		var manifest struct {
			Config struct {
				Digest digest.Digest `json:"digest"`
			}
			Layers []struct {
				digest.Digest `json:"digest"`
			} `json:"layers"`
			Manifests []struct {
				Digest digest.Digest `json:"digest"`
			} `json:"manifests"`
		}

		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			return nil
		}

		// handle image manifest
		if manifest.Config.Digest.Validate() == nil {
			referencedDigests[manifest.Config.Digest.Hex()] = true
		}

		for _, layer := range manifest.Layers {
			if layer.Digest.Validate() == nil {
				referencedDigests[layer.Digest.Hex()] = true
			}
		}

		// handle index list (manifests array)
		for _, m := range manifest.Manifests {
			if m.Digest.Validate() == nil {
				referencedDigests[m.Digest.Hex()] = true
			}
		}

		return nil
	}); err != nil {
		return 0, 0, fmt.Errorf("failed to walk artifacts: %w", err)
	}

	// read all entries
	blobsPath := filepath.Join(l.Root, "blobs", "sha256")
	entries, err := os.ReadDir(blobsPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read blobs directory: %w", err)
	}

	// track count and size of deletions
	deletedCount := 0
	var deletedSize int64

	// scan blobs
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		digest := entry.Name()

		if !referencedDigests[digest] {
			blobPath := filepath.Join(blobsPath, digest)
			if info, err := entry.Info(); err == nil {
				deletedSize += info.Size()
			}

			if err := os.Remove(blobPath); err != nil {
				return deletedCount, deletedSize, fmt.Errorf("failed to remove blob %s: %w", digest, err)
			}
			deletedCount++
		}
	}

	return deletedCount, deletedSize, nil
}
