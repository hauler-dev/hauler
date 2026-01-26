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
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/util/yaml"

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
		} else {
			l.Errorf("unable to parse image [%s]: %v", i.Name, err)
			return err
		}
	}

	// copy and sig verification
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
		rewriteReference(ctx, s, r, newRef)
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

func rewriteReference(ctx context.Context, s *store.Layout, oldRef name.Reference, newRef name.Reference) error {
	l := log.FromContext(ctx)

	l.Infof("rewriting [%s] to [%s]", oldRef.Name(), newRef.Name())

	s.OCI.LoadIndex()

	//TODO: improve string manipulation
	oldRefContext := oldRef.Context()
	newRefContext := newRef.Context()
	oldRepo := oldRefContext.RepositoryStr()
	newRepo := newRefContext.RepositoryStr()
	oldTag := oldRef.(name.Tag).TagStr()
	newTag := newRef.(name.Tag).TagStr()
	oldRegistry := strings.TrimPrefix(oldRefContext.RegistryStr(), "index.")
	newRegistry := strings.TrimPrefix(newRefContext.RegistryStr(), "index.")
	//if new registry not set in rewrite, keep old registry instead of defaulting to docker.io
	if newRegistry == "docker.io" && oldRegistry != "docker.io" {
		newRegistryTrim := strings.TrimPrefix(newRegistry, "docker.io")
		newRegistry = oldRegistry + newRegistryTrim
	}

	oldTotal := oldRepo + ":" + oldTag
	newTotal := newRepo + ":" + newTag
	oldTotalReg := oldRegistry + "/" + oldTotal
	newTotalReg := newRegistry + "/" + newTotal

	//find and update reference
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
	cfg := v1.Chart{
		Name:    chartName,
		RepoURL: o.ChartOpts.RepoURL,
		Version: o.ChartOpts.Version,
	}

	rewrite := ""
	if o.Rewrite != "" {
		rewrite = o.Rewrite
	}
	return storeChart(ctx, s, cfg, o, rso, ro, rewrite)
}

// unexported type for the context key to avoid collisions
type isSubchartKey struct{}

// imageregex parses image references starting with "image:" and with optional spaces or optional quotes
var imageRegex = regexp.MustCompile(`(?m)^[ \t]*image:[ \t]*['"]?([^\s'"#]+)`)

// helmAnnotatedImage parses images references from helm chart annotations
type helmAnnotatedImage struct {
	Image string `yaml:"image"`
	Name  string `yaml:"name,omitempty"`
}

// imagesFromChartAnnotations parses image references from helm chart annotations
func imagesFromChartAnnotations(c *helmchart.Chart) ([]string, error) {
	if c == nil || c.Metadata == nil || c.Metadata.Annotations == nil {
		return nil, nil
	}

	// support multiple annotations
	keys := []string{
		"helm.sh/images",
		"images",
	}

	var out []string
	for _, k := range keys {
		raw, ok := c.Metadata.Annotations[k]
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}

		var items []helmAnnotatedImage
		if err := yaml.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("failed to parse helm chart annotation %q: %w", k, err)
		}

		for _, it := range items {
			img := strings.TrimSpace(it.Image)
			if img == "" {
				continue
			}
			img = strings.TrimPrefix(img, "/")
			out = append(out, img)
		}
	}

	slices.Sort(out)
	out = slices.Compact(out)

	return out, nil
}

// imagesFromImagesLock parses image references from images lock files in the chart directory
func imagesFromImagesLock(chartDir string) ([]string, error) {
	var out []string

	for _, name := range []string{
		"images.lock",
		"images-lock.yaml",
		"images.lock.yaml",
		".images.lock.yaml",
	} {
		p := filepath.Join(chartDir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		matches := imageRegex.FindAllSubmatch(b, -1)
		for _, m := range matches {
			if len(m) > 1 {
				out = append(out, string(m[1]))
			}
		}
	}

	if len(out) == 0 {
		return nil, nil
	}

	for i := range out {
		out[i] = strings.TrimPrefix(out[i], "/")
	}
	slices.Sort(out)
	out = slices.Compact(out)
	return out, nil
}

func applyDefaultRegistry(img string, defaultRegistry string) (string, error) {
	img = strings.TrimSpace(strings.TrimPrefix(img, "/"))
	if img == "" || defaultRegistry == "" {
		return img, nil
	}

	ref, err := reference.Parse(img)
	if err != nil {
		return "", err
	}

	if ref.Context().RegistryStr() != "" {
		return img, nil
	}

	newRef, err := reference.Relocate(img, defaultRegistry)
	if err != nil {
		return "", err
	}

	return newRef.Name(), nil
}

func storeChart(ctx context.Context, s *store.Layout, cfg v1.Chart, opts *flags.AddChartOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string) error {
	l := log.FromContext(ctx)

	// subchart logging prefix
	isSubchart := ctx.Value(isSubchartKey{}) == true
	prefix := ""
	if isSubchart {
		prefix = "  ↳ "
	}

	// normalize chart name for logging
	displayName := cfg.Name
	if strings.Contains(cfg.Name, string(os.PathSeparator)) {
		displayName = filepath.Base(cfg.Name)
	}
	l.Infof("%sadding chart [%s] to the store", prefix, displayName)

	opts.ChartOpts.RepoURL = cfg.RepoURL
	opts.ChartOpts.Version = cfg.Version

	chrt, err := chart.NewChart(cfg.Name, opts.ChartOpts)
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
	if err := s.OCI.SaveIndex(); err != nil {
		return err
	}

	l.Infof("%ssuccessfully added chart [%s:%s]", prefix, c.Name(), c.Metadata.Version)

	tempOverride := rso.TempOverride
	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}
	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
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

		// expanded chart should be in a directory matching the chart name
		expectedChartDir := filepath.Join(tempDir, c.Name())
		if _, err := os.Stat(expectedChartDir); err != nil {
			return fmt.Errorf("chart archive did not expand into expected directory '%s': %w", c.Name(), err)
		}
		chartPath = expectedChartDir
	}

	// add-images
	if opts.AddImages {
		userValues := chartutil.Values{}
		if opts.HelmValues != "" {
			userValues, err = chartutil.ReadValuesFile(opts.HelmValues)
			if err != nil {
				return fmt.Errorf("failed to read helm values file [%s]: %w", opts.HelmValues, err)
			}
		}

		// set helm default capabilities
		caps := chartutil.DefaultCapabilities.Copy()

		// only parse and override if provided kube version
		if opts.KubeVersion != "" {
			kubeVersion, err := chartutil.ParseKubeVersion(opts.KubeVersion)
			if err != nil {
				l.Warnf("%sinvalid kube-version [%s], using default kubernetes version", prefix, opts.KubeVersion)
			} else {
				caps.KubeVersion = *kubeVersion
			}
		}

		values, err := chartutil.ToRenderValues(c, userValues, chartutil.ReleaseOptions{Namespace: "hauler"}, caps)
		if err != nil {
			return err
		}

		// helper for normalization and deduping slices
		normalizeUniq := func(in []string) []string {
			if len(in) == 0 {
				return nil
			}
			for i := range in {
				in[i] = strings.TrimPrefix(in[i], "/")
			}
			slices.Sort(in)
			return slices.Compact(in)
		}

		// Collect images by method so we can debug counts
		var (
			templateImages   []string
			annotationImages []string
			lockImages       []string
		)

		// parse helm chart templates and values for images
		rendered, err := engine.Render(c, values)
		if err != nil {
			// charts may fail due to values so still try helm chart annotations and lock
			l.Warnf("%sfailed to render chart [%s]: %v", prefix, c.Name(), err)
			rendered = map[string]string{}
		}

		for _, manifest := range rendered {
			matches := imageRegex.FindAllStringSubmatch(manifest, -1)
			for _, match := range matches {
				if len(match) > 1 {
					templateImages = append(templateImages, match[1])
				}
			}
		}

		// parse helm chart annotations for images
		annotationImages, err = imagesFromChartAnnotations(c)
		if err != nil {
			l.Warnf("%sfailed to parse helm chart annotation for [%s:%s]: %v", prefix, c.Name(), c.Metadata.Version, err)
			annotationImages = nil
		}

		// parse images lock files for images
		lockImages, err = imagesFromImagesLock(chartPath)
		if err != nil {
			l.Warnf("%sfailed to parse images lock: %v", prefix, err)
			lockImages = nil
		}

		// normalization and deduping the slices
		templateImages = normalizeUniq(templateImages)
		annotationImages = normalizeUniq(annotationImages)
		lockImages = normalizeUniq(lockImages)

		// merge all sources then final dedupe
		images := append(append(templateImages, annotationImages...), lockImages...)
		images = normalizeUniq(images)

		l.Debugf("%simage references identified for helm template: [%d] image(s)", prefix, len(templateImages))

		l.Debugf("%simage references identified for helm chart annotations: [%d] image(s)", prefix, len(annotationImages))

		l.Debugf("%simage references identified for helm image lock file: [%d] image(s)", prefix, len(lockImages))
		l.Debugf("%ssuccessfully parsed and deduped image references: [%d] image(s)", prefix, len(images))

		l.Debugf("%ssuccessfully parsed image references %v", prefix, images)

		if len(images) > 0 {
			l.Infof("%s  ↳ identified [%d] image(s) in [%s:%s]", prefix, len(images), c.Name(), c.Metadata.Version)
		}

		for _, image := range images {
			image, err := applyDefaultRegistry(image, opts.Registry)
			if err != nil {
				if ro.IgnoreErrors {
					l.Warnf("%s  ↳ unable to apply registry to image [%s]: %v... skipping...", prefix, image, err)
					continue
				}
				return fmt.Errorf("unable to apply registry to image [%s]: %w", image, err)
			}

			imgCfg := v1.Image{Name: image}
			if err := storeImage(ctx, s, imgCfg, opts.Platform, rso, ro, ""); err != nil {
				if ro.IgnoreErrors {
					l.Warnf("%s  ↳ failed to store image [%s]: %v... skipping...", prefix, image, err)
					continue
				}
				return fmt.Errorf("failed to store image [%s]: %w", image, err)
			}
			s.OCI.LoadIndex()
			if err := s.OCI.SaveIndex(); err != nil {
				return err
			}
		}
	}

	// add-dependencies
	if opts.AddDependencies && len(c.Metadata.Dependencies) > 0 {
		for _, dep := range c.Metadata.Dependencies {
			l.Infof("%sadding dependent chart [%s:%s]", prefix, dep.Name, dep.Version)

			depOpts := *opts
			depOpts.AddDependencies = true
			depOpts.AddImages = true
			subCtx := context.WithValue(ctx, isSubchartKey{}, true)

			var depCfg v1.Chart
			var err error

			if strings.HasPrefix(dep.Repository, "file://") {
				depPath := strings.TrimPrefix(dep.Repository, "file://")
				subchartPath := filepath.Join(chartPath, depPath)

				depCfg = v1.Chart{Name: subchartPath, RepoURL: "", Version: ""}
				depOpts.ChartOpts.RepoURL = ""
				depOpts.ChartOpts.Version = ""

				err = storeChart(subCtx, s, depCfg, &depOpts, rso, ro, "")
			} else {
				depCfg = v1.Chart{Name: dep.Name, RepoURL: dep.Repository, Version: dep.Version}
				depOpts.ChartOpts.RepoURL = dep.Repository
				depOpts.ChartOpts.Version = dep.Version

				err = storeChart(subCtx, s, depCfg, &depOpts, rso, ro, "")
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

	// chart rewrite functionality
	if rewrite != "" {
		rewrite = strings.TrimPrefix(rewrite, "/")
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			// error... don't continue with a bad reference
			return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
		}

		// if rewrite omits a tag... keep the existing tag
		oldTag := ref.(name.Tag).TagStr()
		if !strings.Contains(rewrite, ":") {
			rewrite = strings.Join([]string{rewrite, oldTag}, ":")
			newRef, err = name.ParseReference(rewrite)
			if err != nil {
				return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
			}
		}

		// rename chart name in store
		s.OCI.LoadIndex()

		oldRefContext := ref.Context()
		newRefContext := newRef.Context()

		oldRepo := oldRefContext.RepositoryStr()
		newRepo := newRefContext.RepositoryStr()
		newTag := newRef.(name.Tag).TagStr()

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

	return nil
}
