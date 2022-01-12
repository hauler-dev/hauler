package store

import (
	"context"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/ocil/pkg/artifacts/file"
	"github.com/rancherfederal/ocil/pkg/artifacts/image"

	"github.com/rancherfederal/ocil/pkg/store"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/reference"
)

type AddFileOpts struct {
	*RootOpts
	Name string
}

func (o *AddFileOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Name, "name", "n", "", "(Optional) Name to assign to file in store")
}

func AddFileCmd(ctx context.Context, o *AddFileOpts, s *store.Layout, reference string) error {
	cfg := v1alpha1.File{
		Path: reference,
	}

	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Layout, fi v1alpha1.File) error {
	l := log.FromContext(ctx)

	f := file.NewFile(fi.Path)
	ref, err := reference.NewTagged(f.Name(fi.Path), reference.DefaultTag)
	if err != nil {
		return err
	}

	desc, err := s.AddOCI(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'file' to store at [%s], with digest [%s]", ref.Name(), desc.Digest.String())
	return nil
}

type AddImageOpts struct {
	*RootOpts
	Name string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f
}

func AddImageCmd(ctx context.Context, o *AddImageOpts, s *store.Layout, reference string) error {
	cfg := v1alpha1.Image{
		Name: reference,
	}

	return storeImage(ctx, s, cfg)
}

func storeImage(ctx context.Context, s *store.Layout, i v1alpha1.Image) error {
	l := log.FromContext(ctx)

	img, err := image.NewImage(i.Name)
	if err != nil {
		return err
	}

	r, err := name.ParseReference(i.Name)
	if err != nil {
		return err
	}

	desc, err := s.AddOCI(ctx, img, r.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'image' to store at [%s], with digest [%s]", r.Name(), desc.Digest.String())
	return nil
}

type AddChartOpts struct {
	*RootOpts
	Version string
	RepoURL string

	// TODO: Support helm auth
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.RepoURL, "repo", "r", "", "Chart repository URL")
	f.StringVar(&o.Version, "version", "", "(Optional) Version of the chart to download, defaults to latest if not specified")
}

func AddChartCmd(ctx context.Context, o *AddChartOpts, s *store.Layout, chartName string) error {
	path := ""
	if _, err := os.Stat(chartName); err == nil {
		path = chartName
	}
	cfg := v1alpha1.Chart{
		Name:    chartName,
		RepoURL: o.RepoURL,
		Version: o.Version,
		Path:    path,
	}

	return storeChart(ctx, s, cfg)
}

func storeChart(ctx context.Context, s *store.Layout, cfg v1alpha1.Chart) error {
	l := log.FromContext(ctx)

	chrt, err := chart.NewChart(cfg)
	if err != nil {
		return err
	}

	ref, err := reference.NewTagged(chrt.Name, chrt.Version)
	if err != nil {
		return err
	}
	desc, err := s.AddOCI(ctx, chrt, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'chart' to store at [%s], with digest [%s]", ref.Name(), desc.Digest.String())
	return nil
}
