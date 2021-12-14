package store

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/reference"
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
	cfg := v1alpha1.File{
		Path: reference,
	}

	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Store, fi v1alpha1.File) error {
	l := log.FromContext(ctx)

	f := file.NewFile(fi.Path)
	ref, err := reference.NewTagged(f.Name(fi.Path), reference.DefaultTag)
	if err != nil {
		return err
	}

	desc, err := s.AddArtifact(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'file' to store at [%s], with digest [%s]", ref.Name(), desc.Digest.String())
	return nil
}

type AddImageOpts struct {
	Name string

	Platform string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Platform, "platform", "p", "all", "Image's platform, specified as OS/ARCH[/VARIANT].  Defaults to images default.")
}

func AddImageCmd(ctx context.Context, o *AddImageOpts, s *store.Store, reference string) error {
	cfg := v1alpha1.Image{
		Name: reference,
	}

	if o.Platform != "" {
		parts := strings.Split(o.Platform, "/")
		if len(parts) < 2 {
			return fmt.Errorf("failed to parse platform [%s], expected OS/ARCH[/VARIANT]", o.Platform)
		} else if len(parts) > 3 {
			return fmt.Errorf("failed to parse platform [%s], expected OS/ARCH[/VARIANT]", o.Platform)
		}
		cfg.Platform = ocispec.Platform{
			OS:           parts[0],
			Architecture: parts[1],
		}
		if len(parts) > 2 {
			cfg.Platform.Variant = parts[2]
		}
	}

	return storeImage(ctx, s, cfg)
}

func storeImage(ctx context.Context, s *store.Store, i v1alpha1.Image) error {
	l := log.FromContext(ctx)

	var opts []remote.Option
	if i.Platform.OS != "" {
		p := gv1.Platform{
			OS:           i.Platform.OS,
			Architecture: i.Platform.Architecture,
			Variant:      i.Platform.Variant,
		}
		opts = append(opts, remote.WithPlatform(p))
	}

	oci, err := image.NewImage(i.Name, opts...)
	if err != nil {
		return err
	}

	r, err := name.ParseReference(i.Name)
	if err != nil {
		return err
	}

	desc, err := s.AddArtifact(ctx, oci, r.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'image' to store at [%s], with digest [%s]", r.Name(), desc.Digest.String())
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

func storeChart(ctx context.Context, s *store.Store, cfg v1alpha1.Chart) error {
	l := log.FromContext(ctx)

	oci, err := chart.NewChart(cfg)
	if err != nil {
		return err
	}

	ref, err := reference.NewTagged(cfg.Name, cfg.Version)
	if err != nil {
		return err
	}
	desc, err := s.AddArtifact(ctx, oci, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'chart' to store at [%s], with digest [%s]", ref.Name(), desc.Digest.String())
	return nil
}
