package store

import (
	"context"
	"os"

	"github.com/rancher/wrangler/pkg/yaml"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type BuildOpts struct {
	StoreFiles string
}

func (o *BuildOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.StoreFiles, "files", "f", "", "Path to store files")
}

func BuildCmd(ctx context.Context, o *BuildOpts, s *store.Store) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store build`")

	s.Start()
	defer s.Stop()

	cs, err := contentStoreFromConfigFile(ctx, o.StoreFiles)
	if err != nil {
		return err
	}

	l.Infof("Adding %d files to content store", len(cs.Spec.Files))
	for _, fi := range cs.Spec.Files {
		f, err := file.NewFile(fi.Canonical)
		if err != nil {
			return err
		}

		if err = s.AddImage(ctx, f, fi.ToRef()); err != nil {
			return err
		}
	}

	l.Infof("Adding %d images to content store", len(cs.Spec.Images))
	for _, i := range cs.Spec.Images {
		_ = i
	}

	l.Infof("Adding %d charts to content store", len(cs.Spec.Charts))
	for _, c := range cs.Spec.Charts {
		ch, err := chart.NewChart(c.RepoURL, c.Name, c.Version)
		if err != nil {
			return err
		}

		if err = s.AddImage(ctx, ch, c.ToRef()); err != nil {
			return err
		}
	}

	l.Infof("%v", cs)

	return nil
}

func contentStoreFromConfigFile(ctx context.Context, filename string) (*v1alpha1.Store, error) {
	l := log.FromContext(ctx)
	var cs *v1alpha1.Store

	l.Debugf("reading content store configuration from '%s'", filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	if err = yaml.Unmarshal(data, &cs); err != nil {
		return nil, err
	}

	return cs, nil
}
