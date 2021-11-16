package store

import (
	"context"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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
	s.Open()
	defer s.Close()

	cfg := v1alpha1.File{
		Ref:  reference,
		Name: o.Name,
	}

	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Store, fi v1alpha1.File) error {
	l := log.FromContext(ctx)

	if fi.Name == "" {
		base := filepath.Base(fi.Ref)
		fi.Name = filepath.Base(fi.Ref)
		l.Warnf("no name specified for file reference [%s], using base filepath: [%s]", fi.Ref, base)
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

	l.Infof("file [%s] added at: [%s]", ref.Name(), desc.Annotations[ocispec.AnnotationTitle])
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
	s.Open()
	defer s.Close()

	cfg := v1alpha1.Image{
		Ref: reference,
	}

	return storeImage(ctx, s, cfg)
}

func storeImage(ctx context.Context, s *store.Store, i v1alpha1.Image) error {
	l := log.FromContext(ctx)

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

	l.Infof("image [%s] added at: [%s]", ref.Name(), desc.Annotations[ocispec.AnnotationTitle])
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
	l := log.FromContext(ctx)

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

	l.Infof("chart [%s] added at: [%s]", ref.Name(), desc.Annotations[ocispec.AnnotationTitle])
	return nil
}
