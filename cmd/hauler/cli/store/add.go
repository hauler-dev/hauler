package store

import (
	"context"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"helm.sh/helm/v3/pkg/action"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"hauler.dev/go/hauler/pkg/artifacts/file"
	"hauler.dev/go/hauler/pkg/artifacts/file/getter"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content/chart"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

func AddFileCmd(ctx context.Context, o *flags.AddFileOpts, s *store.Layout, reference string) error {
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
	ref, err := reference.NewTagged(f.Name(fi.Path), consts.DefaultTag)
	if err != nil {
		return err
	}

	l.Infof("adding file [%s] to the store as [%s]", fi.Path, ref.Name())
	_, err = s.AddOCI(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("successfully added file [%s]", ref.Name())

	return nil
}

func AddImageCmd(ctx context.Context, o *flags.AddImageOpts, s *store.Layout, reference string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	cfg := v1alpha1.Image{
		Name: reference,
	}

	// Check if the user provided a key.
	if o.Key != "" {
		// verify signature using the provided key.
		err := cosign.VerifySignature(ctx, s, o.Key, cfg.Name, rso, ro)
		if err != nil {
			return err
		}
		l.Infof("signature verified for image [%s]", cfg.Name)
	}

	return storeImage(ctx, s, cfg, o.Platform, rso, ro)
}

func storeImage(ctx context.Context, s *store.Layout, i v1alpha1.Image, platform string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	if !ro.IgnoreErrors {
		envVar := os.Getenv(consts.HaulerIgnoreErrors)
		if envVar == "true" {
			ro.IgnoreErrors = true
		}
	}

	l.Infof("adding image [%s] to the store", i.Name)

	r, err := name.ParseReference(i.Name)
	if err != nil {
		if ro.IgnoreErrors {
			l.Warnf("unable to parse image [%s]: %v... skipping...", i.Name, err)
			return nil
		} else {
			l.Errorf("unable to parse image [%s]: %v", i.Name, err)
			return err
		}
	}

	err = cosign.SaveImage(ctx, s, r.Name(), platform, rso, ro)
	if err != nil {
		if ro.IgnoreErrors {
			l.Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		} else {
			l.Errorf("unable to add image [%s] to store: %v", r.Name(), err)
			return err
		}
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

func AddChartCmd(ctx context.Context, o *flags.AddChartOpts, s *store.Layout, chartName string) error {
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

	l.Infof("adding chart [%s] to the store", cfg.Name)

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

	l.Infof("successfully added chart [%s]", ref.Name())
	return nil
}
