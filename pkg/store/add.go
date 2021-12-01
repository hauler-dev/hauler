package store

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/cache"
	"github.com/rancherfederal/hauler/pkg/layout"
	"github.com/rancherfederal/hauler/pkg/log"
)

// AddArtifact will add an artifact.OCI to the store
//  The method to achieve this is to save artifact.OCI to a temporary directory in an OCI layout compatible form.  Once
//  saved, the entirety of the layout is copied to the store (which is just a registry).  This allows us to not only use
//  strict types to define generic content, but provides a processing pipeline suitable for extensibility.  In the
//  future we'll allow users to define their own content that must adhere either by artifact.OCI or simply an OCI layout.
func (s *Store) AddArtifact(ctx context.Context, oci artifact.OCI, reference name.Reference) (ocispec.Descriptor, error) {
	l := log.FromContext(ctx)

	l.Infof("adding ref %s to store", reference.String())

	if err := s.precheck(); err != nil {
		return ocispec.Descriptor{}, err
	}

	stg, err := newOciStage()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if s.cache != nil {
		cached := cache.Oci(oci, s.cache)
		oci = cached
	}

	pdesc, err := stg.add(ctx, oci, reference)
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if err := stg.commit(ctx, s); err != nil {
		return ocispec.Descriptor{}, nil
	}

	return pdesc, nil
}

// Flush is a fancy name for delete-all-the-things, in this case it's as trivial as deleting everything in the underlying store directory
// 	This can be a highly destructive operation if the store's directory happens to be inline with other non-store contents
// 	To reduce the blast radius and likelihood of deleting things we don't own, Flush explicitly includes docker/registry/v2
// 	in the search dir
func (s *Store) Flush(ctx context.Context) error {
	contentDir := filepath.Join(s.DataDir, "docker", "registry", "v2")
	fs, err := ioutil.ReadDir(contentDir)
	if !os.IsNotExist(err) && err != nil {
		return err
	}

	for _, f := range fs {
		err := os.RemoveAll(filepath.Join(contentDir, f.Name()))
		if err != nil {
			return err
		}
	}

	return nil
}

// AddCollection .
func (s *Store) AddCollection(ctx context.Context, coll artifact.Collection) ([]ocispec.Descriptor, error) {
	if err := s.precheck(); err != nil {
		return nil, err
	}

	cnts, err := coll.Contents()
	if err != nil {
		return nil, err
	}

	for ref, o := range cnts {
		if _, err := s.AddArtifact(ctx, o, ref); err != nil {
			return nil, nil
		}
	}

	return nil, err
}

type stager interface {
	// add adds an artifact.OCI to the stage
	add(artifact.OCI) error

	// commit pushes all the staged contents into the store and closes the stage
	commit(*Store) error

	// close flushes and closes the stage
	close() error
}

type oci struct {
	layout layout.Path
	root   string
}

func (o *oci) add(ctx context.Context, oci artifact.OCI, reference name.Reference) (ocispec.Descriptor, error) {
	mdesc, err := o.layout.WriteOci(oci, reference)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	return mdesc, err
}

func (o *oci) commit(ctx context.Context, s *Store) error {
	defer o.close()
	ts, err := layout.NewOCIStore(o.root)
	if err != nil {
		return err
	}

	if err = layout.Copy(ctx, ts, s.Registry()); err != nil {
		return err
	}
	return err
}

func (o *oci) close() error {
	return os.RemoveAll(o.root)
}

func newOciStage() (*oci, error) {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return nil, err
	}

	l, err := layout.FromPath(tmpdir)
	if err != nil {
		return nil, err
	}

	return &oci{
		layout: l,
		root:   tmpdir,
	}, nil
}
