package str

import (
	"context"
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
	lgr := log.FromContext(ctx)
	lgr.Debugf("running cli command `hauler store add`")

	s.Open()
	defer s.Close()

	cfg := v1alpha1.File{
		Ref:  reference,
		Name: o.Name,
	}

	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Store, fi v1alpha1.File) error {
	lgr := log.FromContext(ctx)

	if fi.Name == "" {
		base := filepath.Base(fi.Ref)
		fi.Name = filepath.Base(fi.Ref)
		lgr.Warnf("no name specified for file reference [%s], using base filepath: [%s]", fi.Ref, base)
	}

	oci, err := file.NewFile(fi.Ref, fi.Name)
	if err != nil {
		return err
	}

	ref, err := name.ParseReference(fi.Name, name.WithDefaultRegistry(""))
	if err != nil {
		return err
	}

	desc, err := s.AddArtifact(ctx, oci, ref)
	if err != nil {
		return err
	}

	lgr.Infof("added file [%s] to store at [%s] with manifest digest [%s]", fi.Ref, ref.Name(), desc.Digest.String())
	return nil
}

type AddImageOpts struct {
	Name string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f
}

func AddImageCmd(ctx context.Context, o *AddImageOpts, s *store.Store, reference string) error {
	lgr := log.FromContext(ctx)
	lgr.Debugf("running cli command `hauler store add image`")

	s.Open()
	defer s.Close()

	cfg := v1alpha1.Image{
		Ref: reference,
	}

	return storeImage(ctx, s, cfg)
}

func storeImage(ctx context.Context, s *store.Store, i v1alpha1.Image) error {
	lgr := log.FromContext(ctx)

	oci, err := image.NewImage(i.Ref)
	if err != nil {
		return err
	}

	ref, err := name.ParseReference(i.Ref)
	if err != nil {
		return err
	}

	desc, err := s.AddArtifact(ctx, oci, ref)
	if err != nil {
		return err
	}

	lgr.Infof("added image [%s] to store at [%s] with manifest digest [%s]", i.Ref, ref.Context().RepositoryStr(), desc.Digest.String())
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

func AddChartCmd(ctx context.Context, o *AddChartOpts, s *store.Store, chartName string) error {
	lgr := log.FromContext(ctx)
	lgr.Debugf("running cli command `hauler store add chart`")

	s.Open()
	defer s.Close()

	cfg := v1alpha1.Chart{
		Name:    chartName,
		RepoURL: o.RepoURL,
		Version: o.Version,
	}

	return storeChart(ctx, s, cfg)
}

func storeChart(ctx context.Context, s *store.Store, ch v1alpha1.Chart) error {
	lgr := log.FromContext(ctx)

	oci, err := chart.NewChart(ch.Name, ch.RepoURL, ch.Version)
	if err != nil {
		return err
	}

	tag := ch.Version
	if tag == "" {
		tag = name.DefaultTag
	}

	ref, err := name.ParseReference(ch.Name, name.WithDefaultRegistry(""), name.WithDefaultTag(tag))
	if err != nil {
		return err
	}

	desc, err := s.AddArtifact(ctx, oci, ref)
	if err != nil {
		return err
	}

	lgr.Infof("added chart [%s] to store at [%s:%s] with manifest digest [%s]", ch.Name, ref.Context().RepositoryStr(), ref.Identifier(), desc.Digest.String())
	return nil
}
