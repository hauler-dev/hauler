package store

import (
	"context"
	"fmt"

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

// func addContent(ctx context.Context, o *AddFileOpts, s *store.Store, meta metav1.TypeMeta) error {
// 	l := log.FromContext(ctx)
//
// 	var (
// 		oci artifact.OCI
// 		loc string
// 	)
//
// 	switch cfg := meta.(type) {
// 	case cfg == v1alpha1.FilesContentKind:
// 		f := file.NewFile(cfg)
// 		oci = f
//
// 		loc = f.Name(reference)
//
// 	case "chart":
// 		oci, err := chart.NewThickChart(ch.Name, ch.RepoURL, ch.Version)
// 		if err != nil {
// 			return err
// 		}
//
// 		tag := ch.Version
// 		if tag == "" {
// 			tag = name.DefaultTag
// 		}
//
// 		ref, err := name.ParseReference(ch.Name, name.WithDefaultRegistry(""), name.WithDefaultTag(tag))
// 		if err != nil {
// 			return err
// 		}
//
//
// 	case "image":
// 		i, err := image.NewImage(reference)
// 		if err != nil {
// 			return err
// 		}
// 		oci = i
//
// 		loc = reference
//
// 	default:
// 		return nil
//
// 	}
// 	ref, err := name.ParseReference(loc, name.WithDefaultRegistry(""))
// 	if err != nil {
// 		return err
// 	}
//
// 	desc, err := s.AddArtifact(ctx, oci, ref)
// 	if err != nil {
// 		return err
// 	}
//
// 	l.Infof("added [%s] of type [%s] to store", ref.Name(), s.Identify(ctx, desc))
// 	return nil
// }

func AddFileCmd(ctx context.Context, o *AddFileOpts, s *store.Store, reference string) error {
	cfg := v1alpha1.File{
		Ref: reference,
	}

	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Store, fi v1alpha1.File) error {
	l := log.FromContext(ctx)

	f := file.NewFile(fi.Ref)

	desc, err := s.AddArtifact(ctx, f, f.Name(fi.Ref))
	if err != nil {
		return err
	}

	l.With(log.Fields{"type": s.Identify(ctx, desc)}).Infof("added [%s] to store", desc.Annotations[ocispec.AnnotationRefName])
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

	desc, err := s.AddArtifact(ctx, oci, i.Ref)
	if err != nil {
		return err
	}

	l.With(log.Fields{"type": s.Identify(ctx, desc)}).Infof("added [%s] to store", i.Ref)
	return nil
}

type AddChartOpts struct {
	Version string
	RepoURL string

	// TODO: Support helm auth
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.RepoURL, "repo", "r", "", "Chart repository URL")
	f.StringVar(&o.Version, "version", "", "(Optional) Version of the chart to download, defaults to latest if not specified")
}

func AddChartCmd(ctx context.Context, o *AddChartOpts, s *store.Store, chartName string) error {
	cfg := v1alpha1.Chart{
		Name:    chartName,
		RepoURL: o.RepoURL,
		Version: o.Version,
	}

	return storeChart(ctx, s, cfg)
}

func storeChart(ctx context.Context, s *store.Store, cfg v1alpha1.Chart) error {
	l := log.FromContext(ctx)

	oci, err := chart.NewChart(cfg.Name, cfg.RepoURL, cfg.Version)
	if err != nil {
		return err
	}

	tag := cfg.Version
	if tag == "" {
		tag = name.DefaultTag
	}

	ref := fmt.Sprintf("%s:%s", cfg.Name, tag)
	desc, err := s.AddArtifact(ctx, oci, ref)
	if err != nil {
		return err
	}

	l.With(log.Fields{"type": s.Identify(ctx, desc)}).Infof("added [%s] to store", ref)
	return nil
}
