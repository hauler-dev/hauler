package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/mitchellh/go-homedir"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/cosign"
	"hauler.dev/go/hauler/v2/pkg/flags"
	"hauler.dev/go/hauler/v2/pkg/getter"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/retry"
)

// SyncOptions contains options for syncing content from a manifest
// into the store. Supports three input modes: products (from a
// product registry), filename (local or remote YAML), and imageTxt
// (local or remote image list).
//
// Input mode selection:
//
//	Exactly one of Products, FileName, or ImageTxt must be non-empty.
//	Calling Sync() with zero or multiple non-empty modes returns an error.
//	DryRun is only valid with Products; using it with other modes returns an error.
//
// URL fetching:
//
// FileName and ImageTxt entries starting with "http://" or "https://"
//
//	are downloaded via pkg/getter's HTTP client into a temporary directory.
//	No authentication is performed for remote URL fetching; callers with
//	private URLs must configure proxy-level authentication or use local files.
//
// Authentication (product mode):
//
// Product mode uses authn.DefaultKeychain for fetching product manifest
// images. No explicit credential fields are provided in the initial release.
// Users with private registries must configure Docker credential helpers
// or ~/.docker/config.json.
//
// Cosign verification:
//
//	When Verify is false (default), images in manifests are added without
//	signature verification. This differs from the CLI which performs
//	verification when key/keyless options are provided. To enable
//	verification, set Verify to true and provide Key or certificate options.
//	Verification support includes key-based and keyless verification with
//	full precedence resolution (per-image > annotation > options).
type SyncOptions struct {
	// Products is a list of "name=version" entries for product registry sync.
	// Mutually exclusive with FileName and ImageTxt.
	Products []string

	// FileName is a list of local file paths or URLs to hauler manifest YAML.
	// Mutually exclusive with Products and ImageTxt.
	FileName []string

	// ImageTxt is a list of local file paths or URLs to image list files.
	// Mutually exclusive with Products and FileName.
	ImageTxt []string

	// ProductRegistry overrides the default product registry URL.
	// Defaults to consts.CarbideRegistry if empty.
	ProductRegistry string

	// Platform is the OCI platform to use for image operations.
	Platform string

	// Registry is the default registry for images without one.
	Registry string

	// ExcludeExtras skips cosign extras when pulling images.
	ExcludeExtras bool

	// DryRun outputs product manifest content to stdout instead of
	// processing it. Only valid with Products.
	DryRun bool

	// IgnoreErrors causes the operation to log a warning and continue
	// on failure instead of returning an error. Also respects the
	// HAULER_IGNORE_ERRORS environment variable.
	IgnoreErrors bool

	// Verify enables cosign signature verification for images in manifests.
	// When false (default), no verification is performed.
	Verify bool

	// Key is the path to a cosign public key file for signature verification.
	Key string

	// KubeVersion overrides the Kubernetes version for Helm template rendering
	// when Sync() calls AddChartWithOpts with AddImages: true.
	// Defaults to v1.34.1 if empty.
	KubeVersion string

	// Retries is the number of retry attempts for image operations.
	// Defaults to consts.DefaultRetries (read at runtime in retry wrapper).
	Retries int

	// Tlog enables transparency log verification for signature verification.
	// Defaults to false (verification proceeds without tlog).
	Tlog bool

	// CertIdentity specifies the identity expected in the certificate.
	// Defaults to "" (keyless verification triggers when non-empty).
	CertIdentity string

	// CertIdentityRegexp specifies a regular expression for the certificate identity.
	// Defaults to "" (keyless verification triggers when non-empty).
	CertIdentityRegexp string

	// CertOidcIssuer specifies the OIDC issuer expected in the certificate.
	// Defaults to "" (keyless verification triggers when non-empty).
	CertOidcIssuer string

	// CertOidcIssuerRegexp specifies a regular expression for the OIDC issuer.
	// Defaults to "" (keyless verification triggers when non-empty).
	CertOidcIssuerRegexp string

	// CertGithubWorkflowRepository specifies the GitHub workflow repository expected in the certificate.
	// Defaults to "" (keyless verification triggers when non-empty).
	CertGithubWorkflowRepository string

	// Rewrite specifies a target registry to rewrite image references to.
	// Defaults to "" (no rewrite).
	Rewrite string

	// TempOverride specifies a temporary directory override.
	// Defaults to "" (falls through to env var).
	TempOverride string

	// Local enables local image support (images already present on the host).
	// Defaults to false (opt-in).
	Local bool
}

// buildStoreRootOpts constructs a *flags.StoreRootOpts from SyncOptions
// for use by pkg/cosign and pkg/retry.
func (o *SyncOptions) buildStoreRootOpts() *flags.StoreRootOpts {
	return &flags.StoreRootOpts{
		Retries:      o.Retries,
		TempOverride: o.TempOverride,
	}
}

// buildCliRootOpts constructs a *flags.CliRootOpts from SyncOptions
// for use by pkg/cosign and pkg/retry.
func (o *SyncOptions) buildCliRootOpts() *flags.CliRootOpts {
	return &flags.CliRootOpts{
		IgnoreErrors: o.IgnoreErrors,
	}
}

// Validate checks that SyncOptions has exactly one input mode set and that
// DryRun is not used with non-Products modes. Returns an error if validation
// fails.
func (o *SyncOptions) Validate() error {
	modeCount := 0
	if len(o.Products) > 0 {
		modeCount++
	}
	if len(o.FileName) > 0 {
		modeCount++
	}
	if len(o.ImageTxt) > 0 {
		modeCount++
	}

	if modeCount == 0 {
		return fmt.Errorf("exactly one input mode must be specified")
	}
	if modeCount > 1 {
		return fmt.Errorf("only one input mode may be specified")
	}
	if o.DryRun && len(o.Products) == 0 {
		return fmt.Errorf("DryRun is only valid with Products mode")
	}

	return nil
}

// Sync synchronizes content from a manifest into the store. It
// supports three mutually exclusive input modes:
//
//   - Products: fetches manifest images from a product registry,
//     extracts the YAML, and dispatches by CRD kind. Uses
//     authn.DefaultKeychain for registry authentication.
//   - FileName: reads local or remote YAML manifests and dispatches.
//     Remote URLs (http:// or https://) are fetched via pkg/getter's
//     HTTP client into a temporary directory.
//   - ImageTxt: reads local or remote image list files and adds each.
//     Remote URLs (http:// or https://) are fetched via pkg/getter's
//     HTTP client into a temporary directory.
//
// The caller must provide a *Layout via the s parameter.
//
// Validation: SyncOptions.Validate() must be called before Sync() or
// Sync() will call it internally and return an error if validation
// fails.
//
// Cosign verification: When opts.Verify is false (default), images
// in manifests are added without signature verification. This differs
// from the CLI which performs verification when key/keyless options
// are provided. Set opts.Verify to true and provide opts.Key for
// basic key-based verification.
func Sync(ctx context.Context, s *Layout, opts SyncOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	l := log.FromContext(ctx)

	// Products mode with DryRun — output manifest content without storing
	if len(opts.Products) > 0 && opts.DryRun {
		for _, productName := range opts.Products {
			name, version, ok := strings.Cut(productName, "=")
			if !ok || name == "" || version == "" {
				return fmt.Errorf("invalid product %q: expected format \"name=version\"", productName)
			}
			tag := strings.ReplaceAll(version, "+", "-")

			productRegistry := opts.ProductRegistry
			if productRegistry == "" {
				productRegistry = consts.CarbideRegistry
			}

			manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", productRegistry, name, tag)
			l.Infof("dry run: would process product manifest [%s]", manifestLoc)
		}
		return nil
	}

	tempOverride := opts.TempOverride
	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}
	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	l.Debugf("using temporary directory at [%s]", tempDir)

	// Build one cosign verifier cache for the whole run. Its ctx is run-scoped:
	// a per-image ctx would cancel registry I/O for every image sharing the
	// same verification config (see pkg/cosign/verifier.go NewVerifier).
	var cache *cosign.Cache
	if opts.Verify {
		cache = cosign.NewCache(opts.buildStoreRootOpts(), opts.buildCliRootOpts())
		defer cache.Close()
	}

	// Products mode
	for _, productName := range opts.Products {
		l.Infof("processing product manifest for [%s]", productName)
		name, version, ok := strings.Cut(productName, "=")
		if !ok || name == "" || version == "" {
			return fmt.Errorf("invalid product %q: expected format \"name=version\"", productName)
		}
		tag := strings.ReplaceAll(version, "+", "-")

		productRegistry := opts.ProductRegistry
		if productRegistry == "" {
			productRegistry = consts.CarbideRegistry
		}

		manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", productRegistry, name, tag)
		l.Infof("fetching product manifest from [%s]", manifestLoc)

		parsedRef, err := gname.ParseReference(manifestLoc)
		if err != nil {
			return fmt.Errorf("failed to fetch product manifest for [%s]: %w", productName, err)
		}
		remoteImg, err := remote.Image(parsedRef,
			remote.WithAuthFromKeychain(authn.DefaultKeychain),
			remote.WithContext(ctx),
		)
		if err != nil {
			return fmt.Errorf("failed to fetch product manifest for [%s]: %w", productName, err)
		}

		// Also store the manifest image itself
		img := v1.Image{Name: manifestLoc}
		if err := retryAddImage(ctx, s, img.Name, ImageAddOptions{
			Platform:      opts.Platform,
			ExcludeExtras: opts.ExcludeExtras,
			IgnoreErrors:  opts.IgnoreErrors,
		}, &opts); err != nil {
			return fmt.Errorf("failed to store product manifest image for [%s]: %w", productName, err)
		}

		mf, err := remoteImg.Manifest()
		if err != nil {
			return err
		}
		fileName := fmt.Sprintf("%s-manifest.yaml", name)
		var layerDigest *gv1.Hash
		for _, desc := range mf.Layers {
			if desc.Annotations[ocispec.AnnotationTitle] == fileName {
				layerDigest = &desc.Digest
				break
			}
		}
		if layerDigest == nil {
			return fmt.Errorf("product manifest for [%s] has no layer with title %q", productName, fileName)
		}
		layer, err := remoteImg.LayerByDigest(*layerDigest)
		if err != nil {
			return err
		}
		rc, err := layer.Compressed()
		if err != nil {
			return err
		}
		contentBytes, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}

		// Write manifest to temp file for processing
		manifestPath := filepath.Join(tempDir, fileName)
		if err := os.WriteFile(manifestPath, contentBytes, 0644); err != nil {
			return err
		}

		fi, err := os.Open(manifestPath)
		if err != nil {
			return err
		}
		err = processContent(ctx, fi, &opts, s, cache)
		fi.Close()
		if err != nil {
			return err
		}
		l.Infof("processing completed successfully for [%s]", productName)
	}

	// FileName mode
	for _, fileName := range opts.FileName {
		l.Infof("processing manifest [%s]", fileName)

		haulPath := fileName
		if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
			l.Debugf("detected remote manifest... starting download... [%s]", haulPath)

			h := getter.NewHttp(false, "")
			parsedURL, err := url.Parse(haulPath)
			if err != nil {
				return err
			}
			rc, err := h.Open(ctx, parsedURL)
			if err != nil {
				return err
			}

			baseName := h.Name(parsedURL)
			if baseName == "" {
				baseName = filepath.Base(parsedURL.Path)
			}
			haulPath = filepath.Join(tempDir, baseName)

			out, err := os.Create(haulPath)
			if err != nil {
				rc.Close()
				return err
			}

			if _, err = io.Copy(out, rc); err != nil {
				out.Close()
				rc.Close()
				return err
			}
			out.Close()
			rc.Close()
		}

		fi, err := os.Open(haulPath)
		if err != nil {
			return err
		}

		err = processContent(ctx, fi, &opts, s, cache)
		fi.Close()
		if err != nil {
			return err
		}

		l.Infof("processing completed successfully for [%s]", fileName)
	}

	// ImageTxt mode
	for _, imageTxt := range opts.ImageTxt {
		l.Infof("processing image.txt [%s]", imageTxt)

		haulPath := imageTxt
		if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
			l.Debugf("detected remote image.txt... starting download... [%s]", haulPath)

			h := getter.NewHttp(false, "")
			parsedURL, err := url.Parse(haulPath)
			if err != nil {
				return err
			}
			rc, err := h.Open(ctx, parsedURL)
			if err != nil {
				return err
			}

			baseName := h.Name(parsedURL)
			if baseName == "" {
				baseName = filepath.Base(parsedURL.Path)
			}
			haulPath = filepath.Join(tempDir, baseName)

			out, err := os.Create(haulPath)
			if err != nil {
				rc.Close()
				return err
			}

			if _, err = io.Copy(out, rc); err != nil {
				out.Close()
				rc.Close()
				return err
			}
			out.Close()
			rc.Close()
		}

		fi, err := os.Open(haulPath)
		if err != nil {
			return err
		}

		err = processImageTxt(ctx, fi, &opts, s)
		fi.Close()
		if err != nil {
			return err
		}

		l.Infof("processing completed successfully for [%s]", imageTxt)
	}

	return nil
}

// processContent processes a YAML manifest file, dispatching by CRD kind.
// cache, when non-nil, is the run-scoped cosign verifier cache shared across
// every image in the manifest.
func processContent(ctx context.Context, fi *os.File, opts *SyncOptions, s *Layout, cache *cosign.Cache) error {
	l := log.FromContext(ctx)

	reader := yaml.NewYAMLReader(bufio.NewReader(fi))

	var docs [][]byte
	for {
		raw, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		docs = append(docs, raw)
	}

	for _, doc := range docs {
		obj, err := content.Load(doc)
		if err != nil {
			l.Warnf("skipping syncing due to %v", err)
			continue
		}

		gvk := obj.GroupVersionKind()
		l.Infof("syncing content [%s] with [kind=%s]", gvk.GroupVersion(), gvk.Kind)

		switch gvk.Kind {
		case consts.FilesContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Files
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				for _, f := range cfg.Spec.Files {
					if err := AddFile(ctx, s, f); err != nil {
						if opts.IgnoreErrors || os.Getenv(consts.HaulerIgnoreErrors) == "true" {
							l.Warnf("failed to add file [%s]: %v... skipping...", f.Path, err)
							continue
						}
						return err
					}
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1]", gvk.Version, gvk.Kind)
			}

		case consts.ImagesContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Images
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				a := cfg.GetAnnotations()
				for _, i := range cfg.Spec.Images {
					// Registry relocation
					if !i.Local && (a[consts.ImageAnnotationRegistry] != "" || opts.Registry != "") {
						newRef, err := reference.Parse(i.Name)
						if err != nil {
							if opts.IgnoreErrors || os.Getenv(consts.HaulerIgnoreErrors) == "true" {
								l.Warnf("skipping image [%s]: failed to parse reference: %v", i.Name, err)
								continue
							}
							return fmt.Errorf("failed to parse reference for image [%s]: %w", i.Name, err)
						}
						newReg := opts.Registry
						if opts.Registry == "" && a[consts.ImageAnnotationRegistry] != "" {
							newReg = a[consts.ImageAnnotationRegistry]
						}
						if newRef.Context().RegistryStr() == "" {
							newRef, err = reference.Relocate(i.Name, newReg)
							if err != nil {
								return err
							}
						}
						i.Name = newRef.Name()
					}

					// Local image branch
					if i.Local {
						needsVerif := opts.Key != "" || opts.Tlog || opts.CertIdentity != "" ||
							opts.CertIdentityRegexp != "" || opts.CertOidcIssuer != "" ||
							opts.CertOidcIssuerRegexp != "" || opts.CertGithubWorkflowRepository != ""
						if needsVerif {
							return fmt.Errorf("image %q: --local cannot be combined with cosign verification options", i.Name)
						}

						if _, err := s.AddLocalImage(ctx, i.Name); err != nil {
							if opts.IgnoreErrors || os.Getenv(consts.HaulerIgnoreErrors) == "true" {
								l.Warnf("failed to add local image [%s]: %v... skipping...", i.Name, err)
								continue
							}
							return err
						}
						continue
					}

					// Cosign verification (key-based)
					if opts.Verify && opts.Key != "" {
						key := opts.Key
						if opts.Key == "" && a[consts.ImageAnnotationKey] != "" {
							key = a[consts.ImageAnnotationKey]
						}
						if i.Key != "" {
							key = i.Key
						}
						keyPath, err := homedir.Expand(key)
						if err != nil {
							return err
						}
						tlog := opts.Tlog
						if !opts.Tlog && a[consts.ImageAnnotationTlog] == "true" {
							tlog = true
						}
						if i.Tlog {
							tlog = i.Tlog
						}
						cfg := cosign.Config{Key: keyPath, Tlog: tlog}
						v, err := cache.Get(ctx, cfg)
						if err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						if err := v.Verify(ctx, i.Name); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("signature verified for image [%s]", i.Name)
					}

					// Verify image keyless
					hasAnnotationIdentityOptions := a[consts.ImageAnnotationCertIdentityRegexp] != "" || a[consts.ImageAnnotationCertIdentity] != "" ||
						a[consts.ImageAnnotationCertOidcIssuer] != "" || a[consts.ImageAnnotationCertOidcIssuerRegexp] != "" ||
						a[consts.ImageAnnotationCertGithubWorkflowRepository] != ""
					hasOptsIdentityOptions := opts.CertIdentity != "" || opts.CertIdentityRegexp != "" ||
						opts.CertOidcIssuer != "" || opts.CertOidcIssuerRegexp != "" ||
						opts.CertGithubWorkflowRepository != ""
					hasImageIdentityOptions := i.CertIdentity != "" || i.CertIdentityRegexp != "" ||
						i.CertOidcIssuer != "" || i.CertOidcIssuerRegexp != "" ||
						i.CertGithubWorkflowRepository != ""

					needsKeylessVerification := hasAnnotationIdentityOptions || hasOptsIdentityOptions || hasImageIdentityOptions

					if needsKeylessVerification {
						certIdentityRegexp := opts.CertIdentityRegexp
						if opts.CertIdentityRegexp == "" && a[consts.ImageAnnotationCertIdentityRegexp] != "" {
							certIdentityRegexp = a[consts.ImageAnnotationCertIdentityRegexp]
						}
						if i.CertIdentityRegexp != "" {
							certIdentityRegexp = i.CertIdentityRegexp
						}

						certIdentity := opts.CertIdentity
						if opts.CertIdentity == "" && a[consts.ImageAnnotationCertIdentity] != "" {
							certIdentity = a[consts.ImageAnnotationCertIdentity]
						}
						if i.CertIdentity != "" {
							certIdentity = i.CertIdentity
						}

						certOidcIssuer := opts.CertOidcIssuer
						if opts.CertOidcIssuer == "" && a[consts.ImageAnnotationCertOidcIssuer] != "" {
							certOidcIssuer = a[consts.ImageAnnotationCertOidcIssuer]
						}
						if i.CertOidcIssuer != "" {
							certOidcIssuer = i.CertOidcIssuer
						}

						certOidcIssuerRegexp := opts.CertOidcIssuerRegexp
						if opts.CertOidcIssuerRegexp == "" && a[consts.ImageAnnotationCertOidcIssuerRegexp] != "" {
							certOidcIssuerRegexp = a[consts.ImageAnnotationCertOidcIssuerRegexp]
						}
						if i.CertOidcIssuerRegexp != "" {
							certOidcIssuerRegexp = i.CertOidcIssuerRegexp
						}

						certGithubWorkflowRepository := opts.CertGithubWorkflowRepository
						if opts.CertGithubWorkflowRepository == "" && a[consts.ImageAnnotationCertGithubWorkflowRepository] != "" {
							certGithubWorkflowRepository = a[consts.ImageAnnotationCertGithubWorkflowRepository]
						}
						if i.CertGithubWorkflowRepository != "" {
							certGithubWorkflowRepository = i.CertGithubWorkflowRepository
						}

						cfg := cosign.Config{
							CertIdentity:                 certIdentity,
							CertIdentityRegexp:           certIdentityRegexp,
							CertOidcIssuer:               certOidcIssuer,
							CertOidcIssuerRegexp:         certOidcIssuerRegexp,
							CertGithubWorkflowRepository: certGithubWorkflowRepository,
						}
						v, err := cache.Get(ctx, cfg)
						if err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						if err := v.Verify(ctx, i.Name); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("keyless signature verified for image [%s]", i.Name)
					}

					platform := opts.Platform
					if opts.Platform == "" && a[consts.ImageAnnotationPlatform] != "" {
						platform = a[consts.ImageAnnotationPlatform]
					}
					if i.Platform != "" {
						platform = i.Platform
					}

					excludeExtras := opts.ExcludeExtras
					if !opts.ExcludeExtras && a[consts.ImageAnnotationExcludeExtras] == "true" {
						excludeExtras = true
					}
					if i.ExcludeExtras {
						excludeExtras = i.ExcludeExtras
					}

					finalRewrite := i.Rewrite
					if finalRewrite == "" {
						finalRewrite = opts.Rewrite
					}

					if err := AddImageWithOpts(ctx, s, i.Name, ImageAddOptions{
						Platform:      platform,
						ExcludeExtras: excludeExtras,
						IgnoreErrors:  opts.IgnoreErrors,
						Rewrite:       finalRewrite,
						RawRewrite:    finalRewrite,
					}); err != nil {
						return err
					}
				}
				s.CopyAll(ctx, s.OCI, nil)

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1]", gvk.Version, gvk.Kind)
			}

		case consts.ChartsContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Charts
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				registry := opts.Registry
				annotation := cfg.GetAnnotations()
				if registry == "" {
					if annotation != nil {
						registry = annotation[consts.ImageAnnotationRegistry]
					}
				}

				for i, ch := range cfg.Spec.Charts {
					excludeExtras := opts.ExcludeExtras
					if !opts.ExcludeExtras && annotation != nil && annotation[consts.ImageAnnotationExcludeExtras] == "true" {
						excludeExtras = true
					}
					if ch.ExcludeExtras {
						excludeExtras = ch.ExcludeExtras
					}

					if err := AddChartWithOpts(ctx, s, ch.Name, ChartAddOptions{
						RepoURL:         ch.RepoURL,
						Version:         ch.Version,
						AddImages:       ch.AddImages,
						AddDependencies: ch.AddDependencies,
						ExcludeExtras:   excludeExtras,
						Registry:        registry,
						Platform:        opts.Platform,
						KubeVersion:     opts.KubeVersion,
						IgnoreErrors:    opts.IgnoreErrors,
						Rewrite:         cfg.Spec.Charts[i].Rewrite,
					}); err != nil {
						return err
					}
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1]", gvk.Version, gvk.Kind)
			}

		default:
			return fmt.Errorf("unsupported kind [%s]... valid kinds are [Files, Images, Charts]", gvk.Kind)
		}
	}
	return nil
}

// processImageTxt processes an image.txt file, adding each image to the store.
func processImageTxt(ctx context.Context, fi *os.File, opts *SyncOptions, s *Layout) error {
	l := log.FromContext(ctx)
	l.Infof("syncing images from [%s]", filepath.Base(fi.Name()))
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l.Infof("adding image [%s] to the store", line)
		if err := retryAddImage(ctx, s, line, ImageAddOptions{
			Platform:      opts.Platform,
			ExcludeExtras: opts.ExcludeExtras,
			IgnoreErrors:  opts.IgnoreErrors,
		}, opts); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// retryAddImage wraps AddImageWithOpts in a retry.Operation using the
// provided sync options for retry configuration.
func retryAddImage(ctx context.Context, s *Layout, ref string, opts ImageAddOptions, so *SyncOptions) error {
	rso := so.buildStoreRootOpts()
	ro := so.buildCliRootOpts()
	return retry.Operation(ctx, rso, ro, func() error {
		return AddImageWithOpts(ctx, s, ref, opts)
	})
}
