package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"
	"oras.land/oras-go/pkg/target"

	"github.com/rancherfederal/hauler/internal/cache"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/consts"
)

type Store struct {
	root  string
	store *content.OCI
	cache cache.Cache
}

func NewStore(rootdir string, opts ...Options) (*Store, error) {
	ociStore, err := content.NewOCI(rootdir)
	if err != nil {
		return nil, err
	}

	b := &Store{
		root:  rootdir,
		store: ociStore,
	}

	for _, opt := range opts {
		opt(b)
	}
	return b, nil
}

// AddArtifact will add an artifact.OCI to the store
//  The method to achieve this is to save artifact.OCI to a temporary directory in an OCI layout compatible form.  Once
//  saved, the entirety of the layout is copied to the store (which is just a registry).  This allows us to not only use
//  strict types to define generic content, but provides a processing pipeline suitable for extensibility.  In the
//  future we'll allow users to define their own content that must adhere either by artifact.OCI or simply an OCI layout.
func (s *Store) AddArtifact(ctx context.Context, oci artifact.OCI, reference name.Reference) (ocispec.Descriptor, error) {
	stage, err := newLayout()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if s.cache != nil {
		cached := cache.Oci(oci, s.cache)
		oci = cached
	}

	if err := stage.add(ctx, oci, reference); err != nil {
		return ocispec.Descriptor{}, err
	}
	return stage.commit(ctx, s)
}

// AddCollection .
func (s *Store) AddCollection(ctx context.Context, coll artifact.Collection) ([]ocispec.Descriptor, error) {
	cnts, err := coll.Contents()
	if err != nil {
		return nil, err
	}

	var descs []ocispec.Descriptor
	for ref, oci := range cnts {
		ds, err := s.AddArtifact(ctx, oci, ref)
		if err != nil {
			return nil, err
		}
		descs = append(descs, ds)
	}

	return descs, nil
}

// Flush is a fancy name for delete-all-the-things, in this case it's as trivial as deleting oci-layout content
// 	This can be a highly destructive operation if the store's directory happens to be inline with other non-store contents
// 	To reduce the blast radius and likelihood of deleting things we don't own, Flush explicitly deletes oci-layout content only
func (s *Store) Flush(ctx context.Context) error {
	blobs := filepath.Join(s.root, "blobs")
	if err := os.RemoveAll(blobs); err != nil {
		return err
	}

	index := filepath.Join(s.root, "index.json")
	if err := os.RemoveAll(index); err != nil {
		return err
	}

	layout := filepath.Join(s.root, "oci-layout")
	if err := os.RemoveAll(layout); err != nil {
		return err
	}

	return nil
}

func (s *Store) List(ctx context.Context) ([]ocispec.Descriptor, error) {
	refs := s.store.ListReferences()

	var descs []ocispec.Descriptor
	for _, desc := range refs {
		descs = append(descs, desc)
	}
	return descs, nil
}

func (s *Store) Get(ctx context.Context, to target.Target, reference string) error {
	_, err := oras.Copy(ctx, s.store, reference, to, "",
		oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
	if err != nil {
		return err
	}
	return nil
}

// Copy performs bulk copy operations on the stores oci layout to a provided target.Target
func (s *Store) Copy(ctx context.Context, to target.Target, toMapper func(string) (string, error)) error {
	for ref := range s.store.ListReferences() {
		toRef := ""
		if toMapper != nil {
			tr, err := toMapper(ref)
			if err != nil {
				return err
			}
			toRef = tr
		}

		fmt.Println("copying to: ", toRef)
		_, err := oras.Copy(ctx, s.store, ref, to, toRef,
			oras.WithAdditionalCachedMediaTypes(consts.DockerManifestSchema2))
		if err != nil {
			return err
		}
	}
	return nil
}

// Identify is a helper function that will identify the content type given a descriptor
func (s *Store) Identify(ctx context.Context, desc ocispec.Descriptor) string {
	rc, err := s.store.Fetch(ctx, desc)
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

// RelocateReference returns a name.Reference given a reference and registry
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
