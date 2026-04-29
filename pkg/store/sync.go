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
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	"k8s.io/apimachinery/pkg/util/yaml"

	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
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
//	FileName and ImageTxt entries starting with "http://" or "https://"
//	are downloaded via pkg/getter's HTTP client into a temporary directory.
//	Authentication for remote URLs uses authn.DefaultKeychain.
//
// Authentication (product mode):
//
//	Product mode uses authn.DefaultKeychain for fetching product manifest
//	images. No explicit credential fields are provided in the initial release.
//	Users with private registries must configure Docker credential helpers
//	or ~/.docker/config.json.
//
// Cosign verification:
//
//	When Verify is false (default), images in manifests are added without
//	signature verification. This differs from the CLI which performs
//	verification when key/keyless options are provided. To enable
//	verification, set Verify to true and provide Key or certificate options.
//	Verification support is minimal in the initial release; complex
//	precedence (CLI flag > annotation > per-image setting) is deferred.
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

	tempOverride := os.Getenv(consts.HaulerTempDir)
	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	l.Debugf("using temporary directory at [%s]", tempDir)

	// Products mode
	for _, productName := range opts.Products {
		l.Infof("processing product manifest for [%s]", productName)
		parts := strings.Split(productName, "=")
		tag := strings.ReplaceAll(parts[1], "+", "-")

		productRegistry := opts.ProductRegistry
		if productRegistry == "" {
			productRegistry = consts.CarbideRegistry
		}

		manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", productRegistry, parts[0], tag)
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
		if err := AddImageWithOpts(ctx, s, img.Name, ImageAddOptions{
			Platform:      opts.Platform,
			ExcludeExtras: opts.ExcludeExtras,
			IgnoreErrors:  opts.IgnoreErrors,
		}); err != nil {
			return fmt.Errorf("failed to store product manifest image for [%s]: %w", productName, err)
		}

		mf, err := remoteImg.Manifest()
		if err != nil {
			return err
		}
		fileName := fmt.Sprintf("%s-manifest.yaml", parts[0])
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
		err = processContentPublic(ctx, fi, &opts, s)
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

			h := getter.NewHttp()
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

		err = processContentPublic(ctx, fi, &opts, s)
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

			h := getter.NewHttp()
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

		err = processImageTxtPublic(ctx, fi, &opts, s)
		fi.Close()
		if err != nil {
			return err
		}

		l.Infof("processing completed successfully for [%s]", imageTxt)
	}

	return nil
}

// processContentPublic processes a YAML manifest file, dispatching by CRD kind.
func processContentPublic(ctx context.Context, fi *os.File, opts *SyncOptions, s *Layout) error {
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
						newRef, _ := reference.Parse(i.Name)
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

					// Cosign verification
					if opts.Verify && opts.Key != "" {
						keyPath, err := homedir.Expand(opts.Key)
						if err != nil {
							return err
						}
						l.Debugf("verifying signature for image [%s] with key [%s]", i.Name, keyPath)
						v := &verify.VerifyCommand{
							KeyRef:          keyPath,
							IgnoreTlog:      true,
							NewBundleFormat: true,
						}
						if err := v.Exec(ctx, []string{i.Name}); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("signature verified for image [%s]", i.Name)
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

					if err := AddImageWithOpts(ctx, s, i.Name, ImageAddOptions{
						Platform:      platform,
						ExcludeExtras: excludeExtras,
						IgnoreErrors:  opts.IgnoreErrors,
						Rewrite:       i.Rewrite,
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

// processImageTxtPublic processes an image.txt file, adding each image to the store.
func processImageTxtPublic(ctx context.Context, fi *os.File, opts *SyncOptions, s *Layout) error {
	l := log.FromContext(ctx)
	l.Infof("syncing images from [%s]", filepath.Base(fi.Name()))
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l.Infof("adding image [%s] to the store", line)
		if err := AddImageWithOpts(ctx, s, line, ImageAddOptions{
			Platform:      opts.Platform,
			ExcludeExtras: opts.ExcludeExtras,
			IgnoreErrors:  opts.IgnoreErrors,
		}); err != nil {
			return err
		}
	}
	return scanner.Err()
}
