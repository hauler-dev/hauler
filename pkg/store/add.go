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
	"helm.sh/helm/v3/pkg/action"
	helmchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/util/yaml"

	"hauler.dev/go/hauler/pkg/artifacts/file"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content/chart"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"

	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
)

// AddFile adds a file artifact to the store. The file can be a local
// path or a URL. An OCI tag is generated from the file name.
func AddFile(ctx context.Context, s *Layout, fi v1.File) error {
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
	_, err = s.AddArtifact(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("successfully added file [%s]", ref.Name())

	return nil
}

// ImageAddOptions contains options for adding a container image to the store
// with verification, platform filtering, and reference rewriting.
type ImageAddOptions struct {
	// Platform is the OCI platform to fetch (e.g. "linux/amd64").
	// Empty means all platforms.
	Platform string

	// ExcludeExtras skips cosign signatures, attestations, SBOMs,
	// and OCI referrers when pulling the image.
	ExcludeExtras bool

	// IgnoreErrors causes the operation to log a warning and continue
	// on failure instead of returning an error. Also respects the
	// HAULER_IGNORE_ERRORS environment variable.
	IgnoreErrors bool

	// Rewrite is a new reference to replace the stored image with.
	// If empty, no rewrite is performed.
	Rewrite string

	// RawRewrite is the original user-supplied rewrite string, preserved
	// for registry-preservation logic. Required when Rewrite is non-empty.
	RawRewrite string
}

// AddImageWithOpts adds a container image to the store with optional
// platform filtering, extra artifact exclusion, error tolerance, and
// reference rewriting. It wraps Layout.AddImage() with verification-
// aware error handling.
//
// Retry logic is not included in this function. Callers who need retries
// should wrap the call themselves.
func AddImageWithOpts(ctx context.Context, s *Layout, ref string, opts ImageAddOptions) error {
	l := log.FromContext(ctx)

	ignoreErrors := opts.IgnoreErrors
	if !ignoreErrors {
		envVar := os.Getenv(consts.HaulerIgnoreErrors)
		if envVar == "true" {
			ignoreErrors = true
		}
	}

	l.Infof("adding image [%s] to the store", ref)

	r, err := name.ParseReference(ref)
	if err != nil {
		if ignoreErrors {
			l.Warnf("unable to parse image [%s]: %v... skipping...", ref, err)
			return nil
		}
		l.Errorf("unable to parse image [%s]: %v", ref, err)
		return err
	}

	// fetch image along with any associated signatures and attestations
	err = s.AddImage(ctx, r.Name(), opts.Platform, opts.ExcludeExtras)
	if err != nil {
		if ignoreErrors {
			l.Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		}
		l.Errorf("unable to add image [%s] to store: %v", r.Name(), err)
		return err
	}

	if opts.Rewrite != "" {
		rawRewrite := opts.RawRewrite
		if rawRewrite == "" {
			rawRewrite = opts.Rewrite
		}
		rewrite := strings.TrimPrefix(opts.Rewrite, "/")
		if !strings.Contains(rewrite, ":") {
			if tag, ok := r.(name.Tag); ok {
				rewrite = rewrite + ":" + tag.TagStr()
			} else {
				return fmt.Errorf("cannot rewrite digest reference [%s] without an explicit tag in the rewrite", r.Name())
			}
		}
		// rename image name in store
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
		}
		if err := RewriteReference(ctx, s, r, newRef, rawRewrite); err != nil {
			return err
		}
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

// RewriteReference updates index annotations to replace an old image
// reference with a new one in the store. The rawRewrite parameter is
// the original user-supplied rewrite string, used to preserve the
// original registry when the user omitted it (e.g., "library/nginx"
// vs "docker.io/library/nginx").
func RewriteReference(ctx context.Context, s *Layout, oldRef name.Reference, newRef name.Reference, rawRewrite string) error {
	l := log.FromContext(ctx)

	if err := s.OCI.LoadIndex(); err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	//TODO: improve string manipulation
	oldRefContext := oldRef.Context()
	newRefContext := newRef.Context()
	oldRepo := oldRefContext.RepositoryStr()
	newRepo := newRefContext.RepositoryStr()

	oldTag := oldRef.Identifier()
	if tag, ok := oldRef.(name.Tag); ok {
		oldTag = tag.TagStr()
	}
	newTag := newRef.Identifier()
	if tag, ok := newRef.(name.Tag); ok {
		newTag = tag.TagStr()
	}

	// ContainerdImageNameKey stores annotationRef.Name() verbatim, which includes the
	// "index.docker.io" prefix for docker.io images. Do not strip "index." here or the
	// comparison will never match images stored by writeImage/writeIndex.
	oldRegistry := oldRefContext.RegistryStr()
	newRegistry := newRefContext.RegistryStr()
	// If user omitted a registry in the rewrite string, go-containerregistry defaults to
	// index.docker.io. Preserve the original registry when the source is non-docker.
	if newRegistry == "index.docker.io" && !strings.HasPrefix(rawRewrite, "docker.io") && !strings.HasPrefix(rawRewrite, "index.docker.io") {
		newRegistry = oldRegistry
		newRepo = strings.TrimPrefix(newRepo, "library/") //if rewrite has library/ prefix in path it is stripped off unless registry specified in rewrite
	}
	oldTotal := oldRepo + ":" + oldTag
	newTotal := newRepo + ":" + newTag
	oldTotalReg := oldRegistry + "/" + oldTotal
	newTotalReg := newRegistry + "/" + newTotal

	l.Infof("rewriting [%s] to [%s]", oldTotalReg, newTotalReg)

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

// --------------------------------------------------------------------------
// Chart exports
// --------------------------------------------------------------------------

// ChartAddOptions contains options for adding a Helm chart to the store
// with optional image discovery and dependency resolution.
type ChartAddOptions struct {
	// RepoURL is the chart repository URL (https://, http://, or oci://).
	RepoURL string

	// Version is the chart version to fetch. Empty means latest.
	Version string

	// AddImages extracts container image references from the rendered
	// Helm templates, annotations, and images lock files, then adds
	// them to the store.
	AddImages bool

	// AddDependencies recursively fetches and adds dependent charts.
	AddDependencies bool

	// ExcludeExtras skips cosign signatures, attestations, SBOMs,
	// and OCI referrers when pulling images discovered via AddImages.
	ExcludeExtras bool

	// Registry is the default registry to use for images that do not
	// already define one. Applied via ApplyDefaultRegistry().
	Registry string

	// Platform is the OCI platform to use when pulling images discovered
	// via AddImages.
	Platform string

	// HelmValues is a path to a values file for Helm template rendering
	// when AddImages is true.
	HelmValues string

	// KubeVersion overrides the Kubernetes version for Helm template rendering.
	// Defaults to v1.34.1 if empty.
	KubeVersion string

	// Rewrite is a new reference to replace the stored chart with.
	// If empty, no rewrite is performed.
	Rewrite string

	// IgnoreErrors causes the operation to log a warning and continue
	// on failure instead of returning an error. Also respects the
	// HAULER_IGNORE_ERRORS environment variable.
	IgnoreErrors bool
}

// chartImageRegex parses image references starting with "image:" and with optional spaces or optional quotes
var chartImageRegex = regexp.MustCompile(`(?m)^[ \t]*image:[ \t]*['"]?([^\s'"#]+)`)

// chartHelmAnnotatedImage parses images references from helm chart annotations
type chartHelmAnnotatedImage struct {
	Image string `yaml:"image"`
	Name  string `yaml:"name,omitempty"`
}

// ImagesFromChartAnnotations parses image references from Helm chart
// metadata annotations (helm.sh/images or images keys).
func ImagesFromChartAnnotations(c *helmchart.Chart) ([]string, error) {
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

		var items []chartHelmAnnotatedImage
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

// ImagesFromImagesLock parses image references from images lock files
// in the chart directory (images.lock, images-lock.yaml, etc.).
func ImagesFromImagesLock(chartDir string) ([]string, error) {
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

		matches := chartImageRegex.FindAllSubmatch(b, -1)
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

// ApplyDefaultRegistry prepends a default registry to an image
// reference that does not already have one.
func ApplyDefaultRegistry(img, defaultRegistry string) (string, error) {
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

// AddChartWithOpts adds a Helm chart to the store. If opts.AddImages
// is true, container images referenced in the chart's templates,
// annotations, and lock files are also added. If opts.AddDependencies
// is true, dependent charts are recursively fetched and added.
func AddChartWithOpts(ctx context.Context, s *Layout, chartRef string, opts ChartAddOptions) error {
	l := log.FromContext(ctx)

	ignoreErrors := opts.IgnoreErrors
	if !ignoreErrors {
		envVar := os.Getenv(consts.HaulerIgnoreErrors)
		if envVar == "true" {
			ignoreErrors = true
		}
	}

	// normalize chart name for logging
	displayName := chartRef
	if strings.Contains(chartRef, string(os.PathSeparator)) {
		displayName = filepath.Base(chartRef)
	}
	l.Infof("adding chart [%s] to the store", displayName)

	chartOpts := &action.ChartPathOptions{
		RepoURL: opts.RepoURL,
		Version: opts.Version,
	}

	chrt, err := chart.NewChart(chartRef, chartOpts)
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

	if _, err := s.AddArtifact(ctx, chrt, ref.Name()); err != nil {
		return err
	}
	if err := s.OCI.SaveIndex(); err != nil {
		return err
	}

	l.Infof("successfully added chart [%s:%s]", c.Name(), c.Metadata.Version)

	tempOverride := os.Getenv(consts.HaulerTempDir)
	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	chartPath := chrt.Path()
	if strings.HasSuffix(chartPath, ".tgz") {
		l.Debugf("extracting chart archive [%s]", filepath.Base(chartPath))
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
				l.Warnf("invalid kube-version [%s], using default kubernetes version", opts.KubeVersion)
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
			l.Warnf("failed to render chart [%s]: %v", c.Name(), err)
			rendered = map[string]string{}
		}

		for _, manifest := range rendered {
			matches := chartImageRegex.FindAllStringSubmatch(manifest, -1)
			for _, match := range matches {
				if len(match) > 1 {
					templateImages = append(templateImages, match[1])
				}
			}
		}

		// parse helm chart annotations for images
		annotationImages, err = ImagesFromChartAnnotations(c)
		if err != nil {
			l.Warnf("failed to parse helm chart annotation for [%s:%s]: %v", c.Name(), c.Metadata.Version, err)
			annotationImages = nil
		}

		// parse images lock files for images
		lockImages, err = ImagesFromImagesLock(chartPath)
		if err != nil {
			l.Warnf("failed to parse images lock: %v", err)
			lockImages = nil
		}

		// normalization and deduping the slices
		templateImages = normalizeUniq(templateImages)
		annotationImages = normalizeUniq(annotationImages)
		lockImages = normalizeUniq(lockImages)

		// merge all sources then final dedupe
		images := append(append(templateImages, annotationImages...), lockImages...)
		images = normalizeUniq(images)

		l.Debugf("image references identified for helm template: [%d] image(s)", len(templateImages))
		l.Debugf("image references identified for helm chart annotations: [%d] image(s)", len(annotationImages))
		l.Debugf("image references identified for helm image lock file: [%d] image(s)", len(lockImages))
		l.Debugf("successfully parsed and deduped image references: [%d] image(s)", len(images))
		l.Debugf("successfully parsed image references %v", images)

		if len(images) > 0 {
			l.Infof("  ↳ identified [%d] image(s) in [%s:%s]", len(images), c.Name(), c.Metadata.Version)
		}

		for _, image := range images {
			image, err := ApplyDefaultRegistry(image, opts.Registry)
			if err != nil {
				if ignoreErrors {
					l.Warnf("  ↳ unable to apply registry to image [%s]: %v... skipping...", image, err)
					continue
				}
				return fmt.Errorf("unable to apply registry to image [%s]: %w", image, err)
			}

			imgCfg := v1.Image{Name: image}
			if err := storeImageFromLayout(ctx, s, imgCfg, opts.Platform, opts.ExcludeExtras, ignoreErrors); err != nil {
				if ignoreErrors {
					l.Warnf("  ↳ failed to store image [%s]: %v... skipping...", image, err)
					continue
				}
				return fmt.Errorf("failed to store image [%s]: %w", image, err)
			}
			if err := s.OCI.LoadIndex(); err != nil {
				return err
			}
			if err := s.OCI.SaveIndex(); err != nil {
				return err
			}
		}
	}

	// add-dependencies
	if opts.AddDependencies && len(c.Metadata.Dependencies) > 0 {
		for _, dep := range c.Metadata.Dependencies {
			l.Infof("adding dependent chart [%s:%s]", dep.Name, dep.Version)

			depCfg := ChartAddOptions{
				RepoURL:         opts.RepoURL,
				Version:         opts.Version,
				AddImages:       true,
				AddDependencies: true,
				ExcludeExtras:   opts.ExcludeExtras,
				Registry:        opts.Registry,
				Platform:        opts.Platform,
				HelmValues:      opts.HelmValues,
				KubeVersion:     opts.KubeVersion,
				IgnoreErrors:    opts.IgnoreErrors,
			}

			var depChartRef string
			var err error

			if strings.HasPrefix(dep.Repository, "file://") || dep.Repository == "" {
				subchartPath := filepath.Join(chartPath, "charts", dep.Name)
				depCfg.RepoURL = ""
				depCfg.Version = ""
				depChartRef = subchartPath
			} else {
				depCfg.RepoURL = dep.Repository
				depCfg.Version = dep.Version
				depChartRef = dep.Name
			}

			err = AddChartWithOpts(ctx, s, depChartRef, depCfg)
			if err != nil {
				if ignoreErrors {
					l.Warnf("  ↳ failed to add dependent chart [%s]: %v... skipping...", dep.Name, err)
				} else {
					l.Errorf("  ↳ failed to add dependent chart [%s]: %v", dep.Name, err)
					return err
				}
			}
		}
	}

	// chart rewrite functionality
	if opts.Rewrite != "" {
		rewrite := strings.TrimPrefix(opts.Rewrite, "/")
		newRef, err := name.ParseReference(rewrite)
		if err != nil {
			// error... don't continue with a bad reference
			return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
		}

		// if rewrite omits a tag... keep the existing tag
		oldTag := ref.Identifier()
		if tag, ok := ref.(name.Tag); ok {
			oldTag = tag.TagStr()
		}
		if !strings.Contains(rewrite, ":") {
			rewrite = strings.Join([]string{rewrite, oldTag}, ":")
			newRef, err = name.ParseReference(rewrite)
			if err != nil {
				return fmt.Errorf("unable to parse rewrite name [%s]: %w", rewrite, err)
			}
		}

		// rename chart name in store
		if err := s.OCI.LoadIndex(); err != nil {
			return err
		}

		oldRefContext := ref.Context()
		newRefContext := newRef.Context()

		oldRepo := oldRefContext.RepositoryStr()
		newRepo := newRefContext.RepositoryStr()
		newTag := newRef.Identifier()
		if tag, ok := newRef.(name.Tag); ok {
			newTag = tag.TagStr()
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

	return nil
}

// storeImageFromLayout adds an image to the store using Layout.AddImage,
// with error handling based on ignoreErrors. This is an internal helper
// used by AddChartWithOpts for discovered images.
func storeImageFromLayout(ctx context.Context, s *Layout, i v1.Image, platform string, excludeExtras bool, ignoreErrors bool) error {
	l := log.FromContext(ctx)

	r, err := name.ParseReference(i.Name)
	if err != nil {
		if ignoreErrors {
			l.Warnf("unable to parse image [%s]: %v... skipping...", i.Name, err)
			return nil
		}
		l.Errorf("unable to parse image [%s]: %v", i.Name, err)
		return err
	}

	err = s.AddImage(ctx, r.Name(), platform, excludeExtras)
	if err != nil {
		if ignoreErrors {
			l.Warnf("unable to add image [%s] to store: %v... skipping...", r.Name(), err)
			return nil
		}
		l.Errorf("unable to add image [%s] to store: %v", r.Name(), err)
		return err
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}
