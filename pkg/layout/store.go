package layout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/content/local"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	orascontent "oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

type OCIStore struct {
	content.Store

	root      string
	index     *ocispec.Index
	digestMap map[string]ocispec.Descriptor
}

// Copy placeholder until we migrate to oras 0.5
// Will loop through each appropriately named index and copy the contents to the desired registry
func Copy(ctx context.Context, s *OCIStore, registry string) error {
	for _, desc := range s.index.Manifests {
		manifestBlobPath, err := s.blobPath(desc.Digest)
		if err != nil {
			return err
		}

		manifestData, err := os.ReadFile(manifestBlobPath)
		if err != nil {
			return err
		}

		m, mdesc, err := loadManifest(manifestData)
		if err != nil {
			return err
		}

		refName, ok := desc.Annotations[ocispec.AnnotationRefName]
		if !ok {
			return fmt.Errorf("no name found to push image")
		}

		rref, err := RelocateReference(refName, registry)
		if err != nil {
			return err
		}

		resolver := docker.NewResolver(docker.ResolverOptions{})
		_, err = oras.Push(ctx, resolver, rref.Name(), s, m.Layers,
			oras.WithConfig(m.Config), oras.WithNameValidation(nil), oras.WithManifest(mdesc))

		if err != nil {
			return err
		}
	}

	return nil
}

func NewOCIStore(rootPath string) (*OCIStore, error) {
	fs, err := local.NewStore(rootPath)
	if err != nil {
		return nil, err
	}

	store := &OCIStore{
		Store: fs,

		root: rootPath,
	}

	if err := store.validateOCILayout(); err != nil {
		return nil, err
	}
	if err := store.LoadIndex(); err != nil {
		return nil, nil
	}

	return store, nil
}

func (s *OCIStore) LoadIndex() error {
	path := filepath.Join(s.root, types.OCIImageIndexFile)
	indexFile, err := os.Open(path)
	if err != nil {
		// TODO: Don't just bomb out?
		return err
	}
	defer indexFile.Close()

	if err := json.NewDecoder(indexFile).Decode(&s.index); err != nil {
		return err
	}

	s.digestMap = make(map[string]ocispec.Descriptor)
	for _, desc := range s.index.Manifests {
		if name := desc.Annotations[ocispec.AnnotationRefName]; name != "" {
			s.digestMap[name] = desc
		}
	}

	return nil
}

func (s *OCIStore) validateOCILayout() error {
	layoutFilePath := filepath.Join(s.root, ocispec.ImageLayoutFile)
	layoutFile, err := os.Open(layoutFilePath)
	if err != nil {
		return err
	}
	defer layoutFile.Close()

	var layout *ocispec.ImageLayout
	if err := json.NewDecoder(layoutFile).Decode(&layout); err != nil {
		return err
	}

	if layout.Version != ocispec.ImageLayoutVersion {
		return orascontent.ErrUnsupportedVersion
	}

	return nil
}

func (s *OCIStore) blobPath(d digest.Digest) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}

	return filepath.Join(s.root, "blobs", d.Algorithm().String(), d.Hex()), nil
}

// manifest is a field wrapper around ocispec.Manifest that contains the mediaType field
type manifest struct {
	ocispec.Manifest `json:",inline"`

	MediaType string `json:"mediaType"`
}

// loadManifest
func loadManifest(data []byte) (ocispec.Manifest, ocispec.Descriptor, error) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return ocispec.Manifest{}, ocispec.Descriptor{}, err
	}

	desc := ocispec.Descriptor{
		MediaType: m.MediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}

	return m.Manifest, desc, nil
}

func RelocateReference(reference string, registry string) (name.Reference, error) {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	relocated, err := name.ParseReference(ref.Context().RepositoryStr(), name.WithDefaultRegistry(registry))
	if err != nil {
		return nil, err
	}

	if _, err := name.NewDigest(ref.Name()); err == nil {
		return relocated.Context().Digest(ref.Identifier()), nil
	}
	return relocated.Context().Tag(ref.Identifier()), nil
}
