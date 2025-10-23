package store

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/artifacts/file"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content/chart"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

func AddFileCmd(ctx context.Context, o *flags.AddFileOpts, s *store.Layout, reference string) error {
	cfg := v1.File{
		Path: reference,
	}
	if len(o.Name) > 0 {
		cfg.Name = o.Name
	}
	return storeFile(ctx, s, cfg)
}

func storeFile(ctx context.Context, s *store.Layout, fi v1.File) error {
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

	cfg := v1.Image{
		Name: reference,
	}

	// Check if the user provided a key.
	if o.Key != "" {
		// verify signature using the provided key.
		err := cosign.VerifySignature(ctx, s, o.Key, o.Tlog, cfg.Name, rso, ro)
		if err != nil {
			return err
		}
		l.Infof("signature verified for image [%s]", cfg.Name)
	} else if o.CertIdentityRegexp != "" || o.CertIdentity != "" {

		// verify signature using keyless details
		l.Infof("verifying keyless signature for [%s]", cfg.Name)
		err := cosign.VerifyKeylessSignature(ctx, s, o.CertIdentity, o.CertIdentityRegexp, o.CertOidcIssuer, o.CertOidcIssuerRegexp, o.CertGithubWorkflowRepository, o.Tlog, cfg.Name, rso, ro)
		if err != nil {
			return err
		}
		l.Infof("keyless signature verified for image [%s]", cfg.Name)
	}

	return storeImage(ctx, s, cfg, o.Platform, rso, ro)
}

func storeImage(ctx context.Context, s *store.Layout, i v1.Image, platform string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
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
		}
		l.Errorf("unable to parse image [%s]: %v", i.Name, err)
		return err
	}

	err = cosign.SaveImage(ctx, s, r.Name(), platform, rso, ro)
	if err != nil {
		if ro.IgnoreErrors {
			l.Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		}
		l.Errorf("unable to add image [%s] to store: %v", r.Name(), err)
		return err
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

func AddChartCmd(ctx context.Context, o *flags.AddChartOpts, s *store.Layout, chartName string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	return storeChart(ctx, s, chartName, o, rso, ro)
}

func storeChart(ctx context.Context, s *store.Layout, chartName string, opts *flags.AddChartOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	l.Infof("adding chart [%s] to the store", chartName)

	chrt, err := chart.NewChart(chartName, opts.ChartOpts)
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

	// extract images from helm chart template
	if opts.AddImages {
		if opts.HelmValues != "" {
			values, err := chartutil.ReadValuesFile(opts.HelmValues)
			if err != nil {
				return err
			}
			c.Values = values
		}

		// determine which kube version to use for helm template rendering
		kubeVersion, err := chartutil.ParseKubeVersion(opts.KubeVersion)
		if err != nil {
			l.Warnf("invalid kube-version [%s], defaulting to v1.34.1", opts.KubeVersion)
			kubeVersion = &chartutil.KubeVersion{
				Version: "v1.34.1",
				Major:   "1",
				Minor:   "34",
			}
		}

		caps := chartutil.DefaultCapabilities.Copy()
		caps.KubeVersion = *kubeVersion
		l.Debugf("using Kubernetes version [%s] for helm template rendering", kubeVersion.Version)

		values, err := chartutil.ToRenderValues(c, c.Values, chartutil.ReleaseOptions{Namespace: "hauler"}, caps)
		if err != nil {
			return err
		}

		template, err := engine.Render(c, values)
		if err != nil {
			return err
		}

		var images []string
		for _, manifest := range template {
			for _, line := range strings.Split(manifest, "\n") {
				l := strings.ReplaceAll(line, " ", "")
				l = strings.ReplaceAll(l, "\"", "")
				if strings.HasPrefix(l, "image:") {
					images = append(images, l[6:])
				}
			}
		}

		slices.Sort(images)
		images = slices.Compact(images)

		l.Infof("successfully found images %v", images)

		for _, image := range images {
			storeImage(ctx, s, v1.Image{Name: image, Platform: opts.Platform}, opts.Platform, rso, ro)
		}
	}

	return nil
}
