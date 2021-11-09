package store

import (
	"context"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/cache"
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

func AddFileCmd(ctx context.Context, o *AddFileOpts, s *store.Store, c cache.Cache, reference string) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store add`")

	s.Open()
	defer s.Close()

	fname := o.Name
	if o.Name == "" {
		base := filepath.Base(reference)
		// TODO: Warnings for this feel a little bashful...
		l.Warnf("no name specified for file reference [%s], using base filepath: [%s]", reference, base)
		fname = base
	}

	f, err := file.NewFile(reference, fname)
	if err != nil {
		return err
	}

	ref, err := name.ParseReference(fname)
	if err != nil {
		return err
	}

	if c != nil {
		cf := cache.Oci(f, c)
		f = cf
	}

	if err := s.Add(ctx, f, ref); err != nil {
		return err
	}

	return nil
}

type AddImageOpts struct {
	Name string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f
}

func AddImageCmd(ctx context.Context, o *AddImageOpts, s *store.Store, c cache.Cache, reference string) error {
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

	if c != nil {
		ci := cache.Oci(i, c)
		i = ci
	}

	if err := s.Add(ctx, i, ref); err != nil {
		return err
	}

	return nil
}

type AddChartOpts struct {
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

func AddChartCmd(ctx context.Context, o *AddChartOpts, s *store.Store, c cache.Cache, chartName string) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store add chart`")

	s.Open()
	defer s.Close()

	ch, err := chart.NewChart(chartName, o.RepoURL, o.Version)
	if err != nil {
		return err
	}

	tag := o.Version
	if tag == "" {
		tag = name.DefaultTag
	}
	ref, err := name.ParseReference(chartName, name.WithDefaultTag(tag))
	if err != nil {
		return err
	}

	l.Infof("Adding chart [%s:%s] (%s) store at [%s:%s]",
		chartName, ref.Identifier(), o.RepoURL, ref.Context().RepositoryStr(), ref.Identifier())

	if c != nil {
		cch := cache.Oci(ch, c)
		ch = cch
	}

	if err := s.Add(ctx, ch, ref); err != nil {
		return err
	}

	return nil
}
