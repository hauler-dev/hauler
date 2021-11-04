package store

import (
	"context"
	"path"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type AddFileOpts struct {
	Name string
}

func (o *AddFileOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Name, "name", "n", "", "(Optional) Name to assign to file in store")
}

func AddFileCmd(ctx context.Context, o *AddFileOpts, s *store.Store, reference string) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store add`")

	s.Open()
	defer s.Close()

	cfg := v1alpha1.File{
		Ref:  reference,
		Name: o.Name,
	}

	f, err := file.NewFile(cfg.Ref)
	if err != nil {
		return err
	}

	// TODO: Better way of identifying file content references
	ref, err := name.ParseReference(path.Join("hauler", filepath.Base(reference)))
	if err != nil {
		return err
	}
	return s.Add(ctx, f, ref)
}

type AddImageOpts struct {
	Name string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f
}

func AddImageCmd(ctx context.Context, o *AddImageOpts, s *store.Store, reference string) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store add image`")

	s.Open()
	defer s.Close()

	cfg := v1alpha1.Image{
		Ref: reference,
	}

	i, err := image.NewImage(cfg.Ref)
	if err != nil {
		return err
	}

	ref, err := name.ParseReference(cfg.Ref)
	if err != nil {
		return err
	}

	return s.Add(ctx, i, ref)
}

type AddChartOpts struct {
	Name    string
	Version string
	RepoURL string

	// TODO: Support helm auth
	Username              string
	Password              string
	PassCredentialsAll    bool
	CertFile              string
	KeyFile               string
	CaFile                string
	InsecureSkipTLSverify bool
	RepositoryConfig      string
	RepositoryCache       string
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.RepoURL, "repo", "r", "", "Chart repository URL")
	f.StringVar(&o.Version, "version", "", "(Optional) Version of the chart to download, defaults to latest if not specified")
}

func AddChartCmd(ctx context.Context, o *AddChartOpts, s *store.Store, chartName string) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store add chart`")

	s.Open()
	defer s.Close()

	cfg := v1alpha1.Chart{
		Name:    chartName,
		RepoURL: o.RepoURL,
		Version: o.Version,
	}

	c, err := chart.NewChart(cfg.Name, cfg.RepoURL, cfg.Version)
	if err != nil {
		return err
	}

	ref, err := name.ParseReference(path.Join("hauler", chartName))
	if err != nil {
		return err
	}

	l.Infof("Adding chart from [%s>%s:%s] to store at [%s:%s]",
		cfg.RepoURL, cfg.Name, cfg.Version, ref.Context().RepositoryStr(), ref.Identifier())

	if err := s.Add(ctx, c, ref); err != nil {
		return err
	}

	return s.Add(ctx, c, ref)
}
