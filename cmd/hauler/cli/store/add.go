package store

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/rancherfederal/hauler/pkg/artifacts/file/getter"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"

	"github.com/rancherfederal/hauler/pkg/artifacts/file"

	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/cosign"
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
	if len(o.Name) > 0 {
		cfg.Name = o.Name
	}
	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Layout, fi v1alpha1.File) error {
	l := log.FromContext(ctx)

	copts := getter.ClientOptions{
		NameOverride: fi.Name,
	}

	f := file.NewFile(fi.Path, file.WithClient(getter.NewClient(copts)))
	ref, err := reference.NewTagged(f.Name(fi.Path), reference.DefaultTag)
	if err != nil {
		return err
	}

	l.Infof("adding 'file' [%s] to the store as [%s]", fi.Path, ref.Name())
	_, err = s.AddOCI(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("successfully added 'file' [%s]", ref.Name())

	return nil
}

type AddImageOpts struct {
	*RootOpts
	Name     string
	Key      string
	Platform string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Path to the key for digital signature verification")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specific platform to save. i.e. linux/amd64. Defaults to all if flag is omitted.")
}

func AddImageCmd(ctx context.Context, o *AddImageOpts, s *store.Layout, reference string) error {
	l := log.FromContext(ctx)
	cfg := v1alpha1.Image{
		Name: reference,
	}

	// Check if the user provided a key.
	if o.Key != "" {
		// verify signature using the provided key.
		err := cosign.VerifySignature(ctx, s, o.Key, cfg.Name)
		if err != nil {
			return err
		}
		l.Infof("signature verified for image [%s]", cfg.Name)
	}

	return storeImage(ctx, s, cfg, o.Platform)
}

func storeImage(ctx context.Context, s *store.Layout, i v1alpha1.Image, platform string) error {
	l := log.FromContext(ctx)
	l.Infof("adding 'image' [%s] to the store", i.Name)

	r, err := name.ParseReference(i.Name)
	if err != nil {
		return err
	}

	err = cosign.SaveImage(ctx, s, r.Name(), platform)
	if err != nil {
		return err
	}

	l.Infof("successfully added 'image' [%s]", r.Name())
	return nil
}

type AddChartOpts struct {
	*RootOpts

	ChartOpts *action.ChartPathOptions
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.ChartOpts.RepoURL, "repo", "", "chart repository url where to locate the requested chart")
	f.StringVar(&o.ChartOpts.Version, "version", "", "specify a version constraint for the chart version to use. This constraint can be a specific tag (e.g. 1.1.1) or it may reference a valid range (e.g. ^2.0.0). If this is not specified, the latest version is used")
	f.BoolVar(&o.ChartOpts.Verify, "verify", false, "verify the package before using it")
	f.StringVar(&o.ChartOpts.Username, "username", "", "chart repository username where to locate the requested chart")
	f.StringVar(&o.ChartOpts.Password, "password", "", "chart repository password where to locate the requested chart")
	f.StringVar(&o.ChartOpts.CertFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&o.ChartOpts.KeyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.BoolVar(&o.ChartOpts.InsecureSkipTLSverify, "insecure-skip-tls-verify", false, "skip tls certificate checks for the chart download")
	f.StringVar(&o.ChartOpts.CaFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")
}

func AddChartCmd(ctx context.Context, o *AddChartOpts, s *store.Layout, chartName string) error {
	// TODO: Reduce duplicates between api chart and upstream helm opts
	cfg := v1alpha1.Chart{
		Name:    chartName,
		RepoURL: o.ChartOpts.RepoURL,
		Version: o.ChartOpts.Version,
	}

	return storeChart(ctx, s, cfg, o.ChartOpts)
}

func storeChart(ctx context.Context, s *store.Layout, cfg v1alpha1.Chart, opts *action.ChartPathOptions) error {
	l := log.FromContext(ctx)
	l.Infof("adding 'chart' [%s] to the store", cfg.Name)

	// TODO: This shouldn't be necessary
	opts.RepoURL = cfg.RepoURL
	opts.Version = cfg.Version

	chrt, err := chart.NewChart(cfg.Name, opts)
	if err != nil {
		return err
	}

	c, err := chrt.Load()
	if err != nil {
		return err
	}

	ref, err := reference.NewTagged(c.Name(), c.Metadata.Version)
	if err != nil {
		return err
	}
	_, err = s.AddOCI(ctx, chrt, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("successfully added 'chart' [%s]", ref.Name())
	return nil
}
