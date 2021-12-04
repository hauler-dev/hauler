package store

import (
	"context"
	"encoding/json"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/internal/mapper"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type ExtractOpts struct {
	DestinationDir string
}

func (o *ExtractOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
}

func ExtractCmd(ctx context.Context, o *ExtractOpts, s *store.Store, reference string) error {
	l := log.FromContext(ctx)

	ref, err := name.ParseReference(reference, name.WithDefaultRegistry(""), name.WithDefaultTag("latest"))
	if err != nil {
		return err
	}

	p, err := layout.FromPath("store")
	if err != nil {
		return err
	}

	ii, _ := p.ImageIndex()
	im, _ := ii.IndexManifest()
	var manifest ocispec.Manifest
	for _, m := range im.Manifests {
		if r, ok := m.Annotations[ocispec.AnnotationRefName]; !ok || r != ref.Name() {
			continue
		}

		desc, err := p.Image(m.Digest)
		if err != nil {
			return err
		}
		l.Infof(m.Annotations[ocispec.AnnotationRefName])

		manifestData, err := desc.RawManifest()
		if err != nil {
			return err
		}

		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			return err
		}
	}

	mapperStore, err := mapper.FromManifest(manifest, o.DestinationDir)
	if err != nil {
		return err
	}

	desc, err := s.Get(ctx, mapperStore, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("downloaded [%s] with digest [%s]", desc.MediaType, desc.Digest.String())
	return nil
}
