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
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/util/yaml"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

func SyncCmd(ctx context.Context, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	// Handle dry-run before any local side effects (temp dirs, store writes).
	if o.DryRun {
		for _, productName := range o.Products {
			parts := strings.Split(productName, "=")
			tag := strings.ReplaceAll(parts[1], "+", "-")

			ProductRegistry := o.ProductRegistry
			if o.ProductRegistry == "" {
				ProductRegistry = consts.CarbideRegistry
			}

			manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", ProductRegistry, parts[0], tag)
			fileName := fmt.Sprintf("%s-manifest.yaml", parts[0])

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
			mf, err := remoteImg.Manifest()
			if err != nil {
				return err
			}
			// Select the layer whose AnnotationTitle matches the expected
			// manifest filename, rather than assuming layer order.
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
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}

			// Ensure each manifest starts with a YAML document separator.
			if !strings.HasPrefix(string(content), "---") {
				content = append([]byte("---\n"), content...)
			}
			if _, err := os.Stdout.Write(content); err != nil {
				return err
			}
		}
		return nil
	}

	tempOverride := rso.TempOverride

	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}

	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	l.Debugf("using temporary directory at [%s]", tempDir)

	// if passed products, check for a remote manifest to retrieve and use
	for _, productName := range o.Products {
		l.Infof("processing product manifest for [%s] to store [%s]", productName, o.StoreDir)
		parts := strings.Split(productName, "=")
		tag := strings.ReplaceAll(parts[1], "+", "-")

		ProductRegistry := o.ProductRegistry // cli flag
		// if no cli flag use CarbideRegistry.
		if o.ProductRegistry == "" {
			ProductRegistry = consts.CarbideRegistry
		}

		manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", ProductRegistry, parts[0], tag)
		l.Infof("fetching product manifest from [%s]", manifestLoc)

		img := v1.Image{
			Name: manifestLoc,
		}
		err := storeImage(ctx, s, img, o.Platform, o.ExcludeExtras, rso, ro, "")
		if err != nil {
			return fmt.Errorf("failed to fetch product manifest for [%s]: %w", productName, err)
		}
		err = ExtractCmd(ctx, &flags.ExtractOpts{StoreRootOpts: o.StoreRootOpts}, s, fmt.Sprintf("hauler/%s-manifest.yaml:%s", parts[0], tag))
		if err != nil {
			return err
		}
		fileName := fmt.Sprintf("%s-manifest.yaml", parts[0])
		fi, err := os.Open(fileName)
		if err != nil {
			return err
		}
		defer fi.Close()
		err = processContent(ctx, fi, o, s, rso, ro)
		if err != nil {
			return err
		}
		l.Infof("processing completed successfully")
	}

	// If passed a hauler manifest, process it
	if len(o.FileName) != 0 {
		for _, fileName := range o.FileName {
			l.Infof("processing manifest [%s] to store [%s]", fileName, o.StoreDir)

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
				defer rc.Close()

				fileName := h.Name(parsedURL)
				if fileName == "" {
					fileName = filepath.Base(parsedURL.Path)
				}
				haulPath = filepath.Join(tempDir, fileName)

				out, err := os.Create(haulPath)
				if err != nil {
					return err
				}
				defer out.Close()

				if _, err = io.Copy(out, rc); err != nil {
					return err
				}
			}

			fi, err := os.Open(haulPath)
			if err != nil {
				return err
			}
			defer fi.Close()

			err = processContent(ctx, fi, o, s, rso, ro)
			if err != nil {
				return err
			}

			l.Infof("processing completed successfully")
		}
	}

	// If passed an image.txt file, process it
	if len(o.ImageTxt) != 0 {
		for _, imageTxt := range o.ImageTxt {
			l.Infof("processing image.txt [%s] to store [%s]", imageTxt, o.StoreDir)

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
				defer rc.Close()

				fileName := h.Name(parsedURL)
				if fileName == "" {
					fileName = filepath.Base(parsedURL.Path)
				}
				haulPath = filepath.Join(tempDir, fileName)

				out, err := os.Create(haulPath)
				if err != nil {
					return err
				}
				defer out.Close()

				if _, err = io.Copy(out, rc); err != nil {
					return err
				}
			}

			fi, err := os.Open(haulPath)
			if err != nil {
				return err
			}
			defer fi.Close()

			err = processImageTxt(ctx, fi, o, s, rso, ro)
			if err != nil {
				return err
			}

			l.Infof("processing completed successfully")
		}
	}

	return nil
}

func processContent(ctx context.Context, fi *os.File, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
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
		l.Infof("syncing content [%s] with [kind=%s] to store [%s]", gvk.GroupVersion(), gvk.Kind, o.StoreDir)

		switch gvk.Kind {

		case consts.FilesContentKind:
			switch gvk.Version {
			case "v1":
				var cfg v1.Files
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				for _, f := range cfg.Spec.Files {
					if err := storeFile(ctx, s, f); err != nil {
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

					if !i.Local && (a[consts.ImageAnnotationRegistry] != "" || o.Registry != "") {
						newRef, _ := reference.Parse(i.Name)
						newReg := o.Registry
						if o.Registry == "" && a[consts.ImageAnnotationRegistry] != "" {
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

					if i.Local {
						needsPubKeyVerification := a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != ""
						needsKeylessVerification := a[consts.ImageAnnotationCertIdentityRegexp] != "" || a[consts.ImageAnnotationCertIdentity] != "" ||
							o.CertIdentityRegexp != "" || o.CertIdentity != "" ||
							i.CertIdentityRegexp != "" || i.CertIdentity != ""
						if needsPubKeyVerification || needsKeylessVerification {
							return fmt.Errorf("image [%s]: --local cannot be combined with cosign verification options", i.Name)
						}

						rewrite := ""
						if i.Rewrite != "" {
							rewrite = i.Rewrite
						}
						if err := storeLocalImage(ctx, s, i, rso, ro, rewrite); err != nil {
							return err
						}
						continue
					}

					hasAnnotationIdentityOptions := a[consts.ImageAnnotationCertIdentityRegexp] != "" || a[consts.ImageAnnotationCertIdentity] != ""
					hasCliIdentityOptions := o.CertIdentityRegexp != "" || o.CertIdentity != ""
					hasImageIdentityOptions := i.CertIdentityRegexp != "" || i.CertIdentity != ""

					needsKeylessVerificaton := hasAnnotationIdentityOptions || hasCliIdentityOptions || hasImageIdentityOptions
					needsPubKeyVerification := a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != ""
					if needsPubKeyVerification {
						key := o.Key
						if o.Key == "" && a[consts.ImageAnnotationKey] != "" {
							key, err = homedir.Expand(a[consts.ImageAnnotationKey])
							if err != nil {
								return err
							}
						}
						if i.Key != "" {
							key, err = homedir.Expand(i.Key)
							if err != nil {
								return err
							}
						}
						l.Debugf("key for image [%s]", key)

						tlog := o.Tlog
						if !o.Tlog && a[consts.ImageAnnotationTlog] == "true" {
							tlog = true
						}
						if i.Tlog {
							tlog = i.Tlog
						}
						l.Debugf("transparency log for verification [%b]", tlog)

						if err := cosign.VerifySignature(ctx, key, tlog, i.Name, rso, ro); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("signature verified for image [%s]", i.Name)
					} else if needsKeylessVerificaton { //Keyless signature verification
						certIdentityRegexp := o.CertIdentityRegexp
						if o.CertIdentityRegexp == "" && a[consts.ImageAnnotationCertIdentityRegexp] != "" {
							certIdentityRegexp = a[consts.ImageAnnotationCertIdentityRegexp]
						}
						if i.CertIdentityRegexp != "" {
							certIdentityRegexp = i.CertIdentityRegexp
						}
						l.Debugf("certIdentityRegexp for image [%s]", certIdentityRegexp)

						certIdentity := o.CertIdentity
						if o.CertIdentity == "" && a[consts.ImageAnnotationCertIdentity] != "" {
							certIdentity = a[consts.ImageAnnotationCertIdentity]
						}
						if i.CertIdentity != "" {
							certIdentity = i.CertIdentity
						}
						l.Debugf("certIdentity for image [%s]", certIdentity)

						certOidcIssuer := o.CertOidcIssuer
						if o.CertOidcIssuer == "" && a[consts.ImageAnnotationCertOidcIssuer] != "" {
							certOidcIssuer = a[consts.ImageAnnotationCertOidcIssuer]
						}
						if i.CertOidcIssuer != "" {
							certOidcIssuer = i.CertOidcIssuer
						}
						l.Debugf("certOidcIssuer for image [%s]", certOidcIssuer)

						certOidcIssuerRegexp := o.CertOidcIssuerRegexp
						if o.CertOidcIssuerRegexp == "" && a[consts.ImageAnnotationCertOidcIssuerRegexp] != "" {
							certOidcIssuerRegexp = a[consts.ImageAnnotationCertOidcIssuerRegexp]
						}
						if i.CertOidcIssuerRegexp != "" {
							certOidcIssuerRegexp = i.CertOidcIssuerRegexp
						}
						l.Debugf("certOidcIssuerRegexp for image [%s]", certOidcIssuerRegexp)

						certGithubWorkflowRepository := o.CertGithubWorkflowRepository
						if o.CertGithubWorkflowRepository == "" && a[consts.ImageAnnotationCertGithubWorkflowRepository] != "" {
							certGithubWorkflowRepository = a[consts.ImageAnnotationCertGithubWorkflowRepository]
						}
						if i.CertGithubWorkflowRepository != "" {
							certGithubWorkflowRepository = i.CertGithubWorkflowRepository
						}
						l.Debugf("certGithubWorkflowRepository for image [%s]", certGithubWorkflowRepository)

						// Keyless (Fulcio) certs expire after ~10 min; tlog is always
						// required to prove the cert was valid at signing time.
						if err := cosign.VerifyKeylessSignature(ctx, certIdentity, certIdentityRegexp, certOidcIssuer, certOidcIssuerRegexp, certGithubWorkflowRepository, i.Name, rso, ro); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("keyless signature verified for image [%s]", i.Name)
					}
					platform := o.Platform
					if o.Platform == "" && a[consts.ImageAnnotationPlatform] != "" {
						platform = a[consts.ImageAnnotationPlatform]
					}
					if i.Platform != "" {
						platform = i.Platform
					}

					rewrite := ""
					if i.Rewrite != "" {
						rewrite = i.Rewrite
					}

					excludeExtras := o.ExcludeExtras
					if !o.ExcludeExtras && a[consts.ImageAnnotationExcludeExtras] == "true" {
						excludeExtras = true
					}
					if i.ExcludeExtras {
						excludeExtras = i.ExcludeExtras
					}

					if err := storeImage(ctx, s, i, platform, excludeExtras, rso, ro, rewrite); err != nil {
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
				registry := o.Registry
				annotation := cfg.GetAnnotations()
				if registry == "" {
					if annotation != nil {
						registry = annotation[consts.ImageAnnotationRegistry]
					}
				}

				for i, ch := range cfg.Spec.Charts {
					// Resolve excludeExtras: per-chart field > chart manifest annotation > CLI flag.
					excludeExtras := o.ExcludeExtras
					if !o.ExcludeExtras && annotation != nil && annotation[consts.ImageAnnotationExcludeExtras] == "true" {
						excludeExtras = true
					}
					if ch.ExcludeExtras {
						excludeExtras = ch.ExcludeExtras
					}

					if err := storeChart(ctx, s, ch,
						&flags.AddChartOpts{
							ChartOpts: &action.ChartPathOptions{
								RepoURL: ch.RepoURL,
								Version: ch.Version,
							},
							AddImages:       ch.AddImages,
							AddDependencies: ch.AddDependencies,
							ExcludeExtras:   excludeExtras,
							Registry:        registry,
							Platform:        o.Platform,
						},
						rso, ro,
						cfg.Spec.Charts[i].Rewrite,
					); err != nil {
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

func processImageTxt(ctx context.Context, fi *os.File, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	l.Infof("syncing images from [%s] to store", filepath.Base(fi.Name()))
	scanner := bufio.NewScanner(fi)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		img := v1.Image{Name: line}
		l.Infof("adding image [%s] to the store [%s]", line, o.StoreDir)
		if err := storeImage(ctx, s, img, o.Platform, o.ExcludeExtras, rso, ro, ""); err != nil {
			return err
		}
	}
	return scanner.Err()
}
