package store

import (
	"context"
	"os"
	"path"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/rancher/wrangler/pkg/yaml"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type SyncOpts struct {
	ContentFiles string
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.ContentFiles, "files", "f", "", "Path to content files")
}

func SyncCmd(ctx context.Context, o *SyncOpts, s *store.Store) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store sync`")

	s.Start()
	defer s.Stop()

	var cnt v1alpha1.Content
	data, err := os.ReadFile(o.ContentFiles)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(data, &cnt); err != nil {
		return err
	}

	l.Infof("Syncing store with lock: '%s'", cnt.Name)

	for _, c := range cnt.Spec.Files {
		f, err := file.NewFile(c, "")
		if err != nil {
			return err
		}

		ref, _ := name.ParseReference(path.Join("hauler", c.Name))
		if err := s.Add(ctx, f, ref); err != nil {
			return err
		}
	}

	for _, c := range cnt.Spec.Images {
		i, err := image.NewImage(c.Ref)
		if err != nil {
			return err
		}

		ref, err := name.ParseReference(c.Ref)
		if err != nil {
			return err
		}

		if err := s.Add(ctx, i, ref); err != nil {
			return err
		}
	}

	return nil
}
