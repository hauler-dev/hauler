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
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/engine"
	"k8s.io/apimachinery/pkg/util/yaml"

	"hauler.dev/go/hauler/v2/pkg/artifacts/chart"
	"hauler.dev/go/hauler/v2/pkg/artifacts/file"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/flags"
	"hauler.dev/go/hauler/v2/pkg/getter"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/retry"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
)

// AddFile adds a file artifact to the store. The file can be a local
// path or a URL. An OCI tag is generated from the file name.
func AddFile(ctx context.Context, s *Layout, fi v1.File) error {
	return StoreFile(ctx, s, fi)
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

	// InsecureSkipTLSVerify disables TLS certificate verification for the
	// SOURCE registry and permits a plain-HTTP fallback (go-containerregistry
	// name.Insecure). Opt-in for self-signed / HTTP-only private registries,
	// e.g. a lab registry holding prebuilt model images. Ignored for local
	// (docker-daemon) adds.
	InsecureSkipTLSVerify bool

	// CaFile is the path to a CA bundle used to verify the SOURCE registry's
	// TLS certificate. Ignored when InsecureSkipTLSVerify is set (insecure
	// takes precedence) and for local (docker-daemon) adds.
	CaFile string

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
	l.Infof("adding image [%s] to the store", ref)

	// No retry — public API documented "no retry" behavior
	v1Img := v1.Image{Name: ref, CaFile: opts.CaFile, InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify}
	ro := &flags.CliRootOpts{IgnoreErrors: opts.IgnoreErrors}

	rewrite := opts.Rewrite
	if opts.RawRewrite != "" {
		rewrite = opts.RawRewrite
	}

	return StoreImageNoRetry(
		ctx, s, v1Img, opts.Platform, opts.ExcludeExtras,
		nil, ro, rewrite,
	)
}

// WriteImage adds img to the store's OCI layout under annotationRef with
// the given kind annotation. If containerdName is empty, it defaults to
// annotationRef.Name() (matching the behavior of remote-pull and
// docker-daemon-pull paths).
//
// This is the public counterpart of the internal writeImage. Use it when
// an image has already been materialized as a v1.Image — e.g. loaded
// from a docker-save tarball via go-containerregistry's tarball package,
// or produced by any other in-memory v1.Image source — and the caller
// wants to add it to the store without re-fetching from a registry.
//
// The resulting OCI index entry is byte-identically structured to entries
// produced by AddImage / AddLocalImage (same KindAnnotationName,
// AnnotationRefName, and ContainerdImageNameKey annotations), so consumers
// downstream of the store cannot distinguish how the image was sourced.
func (l *Layout) WriteImage(ctx context.Context, annotationRef name.Reference, img gv1.Image, kind, containerdName string) error {
	return l.writeImage(ctx, annotationRef, img, kind, containerdName, "")
}

// RewriteReference updates index annotations to replace an old image
// reference with a new one in the store. The rawRewrite parameter is
// the original user-supplied rewrite string, used to preserve the
// original registry when the user omitted it (e.g., "library/nginx"
// vs "docker.io/library/nginx").
func RewriteReference(ctx context.Context, s *Layout, oldRef name.Reference, newRef name.Reference, rawRewrite string) error {
	return RewriteIndexRef(ctx, s, oldRef, newRef, rawRewrite)
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

// ImagesFromChartAnnotations parses image references from Helm chart
// metadata annotations (helm.sh/images or images keys).
func ImagesFromChartAnnotations(c *helmchart.Chart) ([]string, error) {
	return ExtractChartImages(c)
}

// ImagesFromImagesLock parses image references from images lock files
// in the chart directory (images.lock, images-lock.yaml, etc.).
func ImagesFromImagesLock(chartDir string) ([]string, error) {
	return ExtractLockImages(chartDir)
}

// ApplyDefaultRegistry prepends defaultRegistry to image names that do not
// already include a registry component.
func ApplyDefaultRegistry(img, defaultRegistry string) (string, error) {
	return _applyDefaultRegistry(img, defaultRegistry)
}

// _applyDefaultRegistry is the canonical implementation of ApplyDefaultRegistry.
// It must be placed at the end of the file (before helper types) so that
// the wrapper at the top can reference it without forward-reference issues.
func _applyDefaultRegistry(img, defaultRegistry string) (string, error) {
	if img == "" {
		return "", nil
	}
	if defaultRegistry == "" {
		return img, nil
	}

	ref, err := name.ParseReference(img)
	if err != nil {
		return "", err
	}

	reg := ref.Context().Registry.Name()
	if reg == "index.docker.io" || reg == "docker.io" {
		return defaultRegistry + "/" + img, nil
	}

	return img, nil
}

// AddChartWithOpts adds a Helm chart to the store. If opts.AddImages
// is true, container images referenced in the chart's templates,
// annotations, and lock files are also added. If opts.AddDependencies
// is true, dependent charts are recursively fetched and added.
func AddChartWithOpts(ctx context.Context, s *Layout, chartRef string, opts ChartAddOptions) error {
	// Merge ignore-errors env var
	if !opts.IgnoreErrors {
		if os.Getenv(consts.HaulerIgnoreErrors) == "true" {
			opts.IgnoreErrors = true
		}
	}

	cfg := v1.Chart{
		Name:          chartRef,
		RepoURL:       opts.RepoURL,
		Version:       opts.Version,
		AddImages:     opts.AddImages,
		ExcludeExtras: opts.ExcludeExtras,
	}

	var valuesFiles []string
	if opts.HelmValues != "" {
		valuesFiles = []string{opts.HelmValues}
	}

	addOpts := &flags.AddChartOpts{
		ChartOpts: &action.ChartPathOptions{
			RepoURL: opts.RepoURL,
			Version: opts.Version,
		},
		AddImages:       opts.AddImages,
		AddDependencies: opts.AddDependencies,
		ExcludeExtras:   opts.ExcludeExtras,
		ValuesFiles:     valuesFiles,
		Platform:        opts.Platform,
		Registry:        opts.Registry,
		KubeVersion:     opts.KubeVersion,
	}

	ro := &flags.CliRootOpts{IgnoreErrors: opts.IgnoreErrors}
	rso := &flags.StoreRootOpts{}

	// Chart discovery uses StoreImageNoRetry (no retry for discovered images)
	return StoreChart(ctx, s, cfg, addOpts, rso, ro, opts.Rewrite)
}

// StoreFile adds a file artifact to the store. The file can be a local
// path or a URL. An OCI tag is generated from the file name.
func StoreFile(ctx context.Context, s *Layout, fi v1.File) error {
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

// StoreImage adds a container image to the store with optional platform
// filtering, extra artifact exclusion, and reference rewriting. It wraps
// Layout.AddImage() with retry logic and verification-aware error handling.
func StoreImage(ctx context.Context, s *Layout, i v1.Image, platform string, excludeExtras bool, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string) error {
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

	// fetch image along with any associated signatures and attestations
	err = retry.Operation(ctx, rso, ro, func() error {
		_, err := s.AddImage(ctx, r.Name(), platform, excludeExtras, "", i.InsecureSkipTLSVerify, i.CaFile)
		return err
	})
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
		rawRewrite := rewrite
		rewrite = strings.TrimPrefix(rewrite, "/")
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
		if err := RewriteIndexRef(ctx, s, r, newRef, rawRewrite); err != nil {
			return err
		}
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

// StoreImageNoRetry adds a container image to the store without retry logic.
func StoreImageNoRetry(ctx context.Context, s *Layout, i v1.Image, platform string, excludeExtras bool, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string) error {
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

	// fetch image along with any associated signatures and attestations
	_, err = s.AddImage(ctx, r.Name(), platform, excludeExtras, "", i.InsecureSkipTLSVerify, i.CaFile)
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
		rawRewrite := rewrite
		rewrite = strings.TrimPrefix(rewrite, "/")
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
		if err := RewriteIndexRef(ctx, s, r, newRef, rawRewrite); err != nil {
			return err
		}
	}

	l.Infof("successfully added image [%s]", r.Name())
	return nil
}

// RewriteIndexRef updates index annotations to replace an old image
// reference with a new one in the store. The rawRewrite parameter is
// the original user-supplied rewrite string, used to preserve the
// original registry when the user omitted it.
func RewriteIndexRef(ctx context.Context, s *Layout, oldRef name.Reference, newRef name.Reference, rawRewrite string) error {
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

	//find and update reference. Walk hands the callback a deep copy of each
	//descriptor's annotations, so an in-place mutation would be discarded;
	//UpdateAnnotations is the supported way to persist annotation changes.
	matched, err := s.OCI.UpdateAnnotations(
		func(d ocispec.Descriptor) bool {
			return d.Annotations[ocispec.AnnotationRefName] == oldTotal && d.Annotations[consts.ContainerdImageNameKey] == oldTotalReg
		},
		func(ann map[string]string) {
			ann[ocispec.AnnotationRefName] = newTotal
			ann[consts.ContainerdImageNameKey] = newTotalReg
		},
	)
	if err != nil {
		return err
	}

	if matched == 0 {
		return fmt.Errorf("could not find image [%s] in store", oldRef.Name())
	}

	return nil
}

// ExtractChartImages parses image references from Helm chart metadata
// annotations (helm.sh/images or images keys).
func ExtractChartImages(c *helmchart.Chart) ([]string, error) {
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

		var items []ChartAnnotatedImage
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

// ExtractLockImages parses image references from images lock files
// in the chart directory (images.lock, images-lock.yaml, etc.).
func ExtractLockImages(chartDir string) ([]string, error) {
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

		matches := ChartImageRegex.FindAllSubmatch(b, -1)
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

// StoreChart adds a Helm chart to the store. If opts.AddImages
// is true, container images referenced in the chart's templates,
// annotations, and lock files are also added. If opts.AddDependencies
// is true, dependent charts are recursively fetched and added.
func StoreChart(ctx context.Context, s *Layout, cfg v1.Chart, opts *flags.AddChartOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, rewrite string) error {
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

	if _, err := s.AddArtifact(ctx, chrt, ref.Name()); err != nil {
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
		userValues := common.Values{}
		if len(opts.ValuesFiles) > 0 && opts.ValuesFiles[0] != "" {
			userValues, err = common.ReadValuesFile(opts.ValuesFiles[0])
			if err != nil {
				return fmt.Errorf("failed to read helm values file [%s]: %w", opts.ValuesFiles[0], err)
			}
		}

		// set helm default capabilities
		caps := common.DefaultCapabilities.Copy()

		// only parse and override if provided kube version
		if opts.KubeVersion != "" {
			kubeVersion, err := common.ParseKubeVersion(opts.KubeVersion)
			if err != nil {
				l.Warnf("%sinvalid kube-version [%s], using default kubernetes version", prefix, opts.KubeVersion)
			} else {
				caps.KubeVersion = *kubeVersion
			}
		}

		values, err := commonutil.ToRenderValues(c, userValues, common.ReleaseOptions{Namespace: "hauler"}, caps)
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
			matches := ChartImageRegex.FindAllStringSubmatch(manifest, -1)
			for _, match := range matches {
				if len(match) > 1 {
					templateImages = append(templateImages, match[1])
				}
			}
		}

		// parse helm chart annotations for images
		annotationImages, err = ExtractChartImages(c)
		if err != nil {
			l.Warnf("%sfailed to parse helm chart annotation for [%s:%s]: %v", prefix, c.Name(), c.Metadata.Version, err)
			annotationImages = nil
		}

		// parse images lock files for images
		lockImages, err = ExtractLockImages(chartPath)
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
			image, err := ApplyDefaultRegistry(image, opts.Registry)
			if err != nil {
				if ro.IgnoreErrors {
					l.Warnf("%s  ↳ unable to apply registry to image [%s]: %v... skipping...", prefix, image, err)
					continue
				}
				return fmt.Errorf("unable to apply registry to image [%s]: %w", image, err)
			}

			imgCfg := v1.Image{Name: image}
			if err := StoreImage(ctx, s, imgCfg, opts.Platform, opts.ExcludeExtras, rso, ro, ""); err != nil {
				if ro.IgnoreErrors {
					l.Warnf("%s  ↳ failed to store image [%s]: %v... skipping...", prefix, image, err)
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
			l.Infof("%sadding dependent chart [%s:%s]", prefix, dep.Name, dep.Version)

			depOpts := *opts
			depOpts.AddDependencies = true
			depOpts.AddImages = true
			subCtx := context.WithValue(ctx, isSubchartKey{}, true)

			var depCfg v1.Chart
			var err error

			if strings.HasPrefix(dep.Repository, "file://") || dep.Repository == "" {
				subchartPath := filepath.Join(chartPath, "charts", dep.Name)

				depCfg = v1.Chart{Name: subchartPath, RepoURL: "", Version: ""}
				depOpts.ChartOpts.RepoURL = ""
				depOpts.ChartOpts.Version = ""

				err = StoreChart(subCtx, s, depCfg, &depOpts, rso, ro, "")
			} else {
				depCfg = v1.Chart{Name: dep.Name, RepoURL: dep.Repository, Version: dep.Version}
				depOpts.ChartOpts.RepoURL = dep.Repository
				depOpts.ChartOpts.Version = dep.Version

				err = StoreChart(subCtx, s, depCfg, &depOpts, rso, ro, "")
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

		matched, err := s.OCI.UpdateAnnotations(
			func(d ocispec.Descriptor) bool {
				return d.Annotations[ocispec.AnnotationRefName] == oldTotal
			},
			func(ann map[string]string) {
				ann[ocispec.AnnotationRefName] = newTotal
			},
		)
		if err != nil {
			return err
		}

		if matched == 0 {
			return fmt.Errorf("could not find chart [%s] in store", ref.Name())
		}
	}

	return nil
}

// isSubchartKey is an unexported context key for subchart log prefix
type isSubchartKey struct{}

var ChartImageRegex = regexp.MustCompile(`(?m)^[ \t]*image:[ \t]*['"]?([^\s'"#]+)`)

type ChartAnnotatedImage struct {
	Image string `yaml:"image"`
	Name  string `yaml:"name,omitempty"`
}
