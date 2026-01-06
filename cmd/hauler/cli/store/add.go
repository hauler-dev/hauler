package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
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
	"helm.sh/helm/v3/pkg/action"
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
		Name:    reference,
		Rewrite: o.Rewrite,
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

	return storeImage(ctx, s, cfg, o.Platform, rso, ro, o.Rewrite)
}

func storeImage(ctx context.Context, s *store.Layout, i v1.Image, platform string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string) error {
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

	// copy and sig verification
	err = cosign.SaveImage(ctx, s, r.Name(), platform, rso, ro)
	if err != nil {
		if ro.IgnoreErrors {
			l.Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		}
		l.Errorf("unable to add image [%s] to store: %v", r.Name(), err)
		return err
	}

	if rewrite != "" {
		rewrite = strings.TrimPrefix(rewrite, "/")
		if !strings.Contains(rewrite, ":") {
			rewrite = strings.Join([]string{rewrite, r.(name.Tag).TagStr()}, ":")
		}
		// rename image name in store
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			l.Errorf("unable to parse rewrite name: %w", err)
		}
    if err := rewriteReference(ctx, s, r, newRef); err != nil {
      if ro.IgnoreErrors {
        l.Warnf("unable to rewrite reference for image [%s]: %v... skipping...", r.Name(), err)
        return nil
      }
      return err
    }
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

func AddChartCmd(ctx context.Context, o *flags.AddChartOpts, s *store.Layout, chartName string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	return storeChart(ctx, s, chartName, o, rso, ro)
}

// unexported type for the context key to avoid collisions.
type isSubchartKey struct{}

// imageregex finds lines starting with optional space, 'image:', optional space, optional quotes,
// and captures the image name with non-space/non-quote/non-hash chars
var imageRegex = regexp.MustCompile(`(?m)^\s*image:\s*['"]?([^\s'"#]+)`)

func rewriteReference(ctx context.Context, s *store.Layout, oldRef name.Reference, newRef name.Reference) error {
	l := log.FromContext(ctx)

	l.Infof("rewriting [%s] to [%s]", oldRef.Name(), newRef.Name())

	s.OCI.LoadIndex()

	// TODO: improve string manipulation
	oldRefContext := oldRef.Context()
	newRefContext := newRef.Context()

	oldRepo := oldRefContext.RepositoryStr()
	newRepo := newRefContext.RepositoryStr()
	oldTag := oldRef.(name.Tag).TagStr()
	newTag := newRef.(name.Tag).TagStr()
	oldRegistry := strings.TrimPrefix(oldRefContext.RegistryStr(), "index.")
	newRegistry := strings.TrimPrefix(newRefContext.RegistryStr(), "index.")

	oldTotal := oldRepo + ":" + oldTag
	newTotal := newRepo + ":" + newTag
	oldTotalReg := oldRegistry + "/" + oldTotal
	newTotalReg := newRegistry + "/" + newTotal

	// find and update reference
	found := false
	if err := s.OCI.Walk(func(k string, d ocispec.Descriptor) error {
		if d.Annotations[ocispec.AnnotationRefName] == oldTotal && d.Annotations[consts.ContainerdImageNameKey] == oldTotalReg {
			d.Annotations[ocispec.AnnotationRefName] = newTotal
			d.Annotations[consts.ContainerdImageNameKey] = newTotalReg
			found = true
		}
		return nil
	}); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("could not find image [%s] in store", oldRef.Name())
	}

	return s.OCI.SaveIndex()
}

func AddChartCmd(ctx context.Context, o *flags.AddChartOpts, s *store.Layout, chartName string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	return storeChart(ctx, s, chartName, o, rso, ro)
}

func storeChart(ctx context.Context, s *store.Layout, chartName string, opts *flags.AddChartOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	isSubchart := ctx.Value(isSubchartKey{}) == true
	prefix := ""
	if isSubchart {
		prefix = "  ↳ "
	}

	// normalize chart name for logging purposes
	displayName := chartName
	if strings.Contains(chartName, string(os.PathSeparator)) {
		displayName = filepath.Base(chartName)
	}
	l.Infof("%sadding chart [%s] to the store", prefix, displayName)

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

	if _, err := s.AddOCI(ctx, chrt, ref.Name()); err != nil {
		return err
	}
	s.OCI.SaveIndex()

	// --- chart rewrite (keep main behavior) ---
	if opts.Rewrite != "" {
		rewrite := strings.TrimPrefix(opts.Rewrite, "/")
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			return fmt.Errorf("unable to parse rewrite name: %w", err)
		}

		s.OCI.LoadIndex()

		oldRefContext := ref.Context()
		newRefContext := newRef.Context()

		oldRepo := oldRefContext.RepositoryStr()
		newRepo := newRefContext.RepositoryStr()
		oldTag := ref.(name.Tag).TagStr()

		var newTag string
		if strings.Contains(rewrite, ":") {
			newTag = newRef.(name.Tag).TagStr()
		} else {
			newTag = oldTag
		}

		oldTotal := oldRepo + ":" + oldTag
		newTotal := newRepo + ":" + newTag

		found := false
		if err := s.OCI.Walk(func(k string, d ocispec.Descriptor) error {
			if d.Annotations[ocispec.AnnotationRefName] == oldTotal {
				d.Annotations[ocispec.AnnotationRefName] = newTotal
				found = true
			}
			return nil
		}); err != nil {
			return err
		}

		if !found {
			return fmt.Errorf("could not find chart [%s] in store", ref.Name())
		}

		if err := s.OCI.SaveIndex(); err != nil {
			return err
		}
	}

	l.Infof("%ssuccessfully added chart [%s:%s]", prefix, c.Name(), c.Metadata.Version)

	// --- your feature: extract chart images + recurse deps ---
	tempBase := rso.TempOverride
	if tempBase == "" {
		tempBase = os.Getenv(consts.HaulerTempDir)
	}
	tempDir, err := os.MkdirTemp(tempBase, consts.DefaultHaulerTempDirName)
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	chartPath := chrt.Path()
	if strings.HasSuffix(chartPath, ".tgz") {
		l.Debugf("%sextracting chart archive [%s]", prefix, filepath.Base(chartPath))
		if err := chartutil.ExpandFile(tempDir, chartPath); err != nil {
			return fmt.Errorf("failed to extract chart: %w", err)
		}

		// expanded chart should be in a directory matching the chart’s name
		expectedChartDir := filepath.Join(tempDir, c.Name())
		if _, err := os.Stat(expectedChartDir); err != nil {
			return fmt.Errorf("chart archive did not expand into expected directory '%s': %w", c.Name(), err)
		}
		chartPath = expectedChartDir
	}

	if opts.AddImages {
		// load user values from file
		userValues := chartutil.Values{}
		if opts.HelmValues != "" {
			userValues, err = chartutil.ReadValuesFile(opts.HelmValues)
			if err != nil {
				return fmt.Errorf("failed to read helm values file [%s]: %w", opts.HelmValues, err)
			}
		}

		// determine which kube version to use for helm template rendering
		kubeVersion, err := chartutil.ParseKubeVersion(opts.KubeVersion)
		if err != nil {
			l.Warnf("%sinvalid kube-version [%s], using default kubernetes version", prefix, opts.KubeVersion)
			kubeVersion = &chartutil.DefaultCapabilities.KubeVersion
		}

		caps := chartutil.DefaultCapabilities.Copy()
		caps.KubeVersion = *kubeVersion

		// merge defaults with user values
		values, err := chartutil.ToRenderValues(c, userValues, chartutil.ReleaseOptions{Namespace: "hauler"}, caps)
		if err != nil {
			return err
		}

		rendered, err := engine.Render(c, values)
		if err != nil {
			// warning since some charts might fail to render without extensive or non-default values
			l.Warnf("%sfailed to render chart [%s]: %v", prefix, c.Name(), err)
			return nil
		}

		// extract images from rendered manifests
		var images []string
		for _, manifest := range rendered {
			matches := imageRegex.FindAllStringSubmatch(manifest, -1)
			for _, match := range matches {
				if len(match) > 1 {
					images = append(images, match[1])
				}
			}
		}

		slices.Sort(images)
		images = slices.Compact(images)

		l.Debugf("%ssuccessfully parsed image references %v", prefix, images)

		if len(images) > 0 {
			l.Infof("%s  ↳ identified [%d] image(s) in [%s:%s]", prefix, len(images), c.Name(), c.Metadata.Version)
		}

		for _, image := range images {
			imgCfg := v1.Image{Name: image}
			// IMPORTANT: storeImage signature includes rewrite string
			if err := storeImage(ctx, s, imgCfg, opts.Platform, rso, ro, ""); err != nil {
				return fmt.Errorf("failed to store image [%s]: %w", image, err)
			}
		}
	}

	if opts.AddDependencies && len(c.Metadata.Dependencies) > 0 {
		for _, dep := range c.Metadata.Dependencies {
			l.Infof("%sadding dependent chart [%s:%s]", prefix, dep.Name, dep.Version)

			depOpts := *opts
			depOpts.AddDependencies = false
			depOpts.AddImages = false
			subCtx := context.WithValue(ctx, isSubchartKey{}, true)

			var err error
			if strings.HasPrefix(dep.Repository, "file://") {
				depPath := strings.TrimPrefix(dep.Repository, "file://")
				subchartPath := filepath.Join(chartPath, depPath)
				depOpts.ChartOpts.RepoURL = ""
				err = storeChart(subCtx, s, subchartPath, &depOpts, rso, ro)
			} else {
				depOpts.ChartOpts.RepoURL = dep.Repository
				depOpts.ChartOpts.Version = dep.Version
				err = storeChart(subCtx, s, dep.Name, &depOpts, rso, ro)
			}

			if err != nil {
				if ro.IgnoreErrors {
					l.Warnf("%s  ↳ failed to add dependent chart [%s]: %v... skipping...", prefix, dep.Name, err)
				} else {
					l.Errorf("%s  ↳ failed to add dependent chart [%s]: %v", prefix, dep.Name, err)
					return err
				}
			}
		}
	}

	return nil
}
