package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mitchellh/go-homedir"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/util/yaml"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	tchart "hauler.dev/go/hauler/pkg/collection/chart"
	"hauler.dev/go/hauler/pkg/collection/imagetxt"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

func SyncCmd(ctx context.Context, o *flags.SyncOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	// if passed products, check for a remote manifest to retrieve and use.
	for _, product := range o.Products {
		l.Infof("processing content file for product [%s]", product)
		parts := strings.Split(product, "=")
		tag := strings.ReplaceAll(parts[1], "+", "-")

		ProductRegistry := o.ProductRegistry // cli flag
		// if no cli flag use CarbideRegistry.
		if o.ProductRegistry == "" {
			ProductRegistry = consts.CarbideRegistry
		}

		manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", ProductRegistry, parts[0], tag)
		l.Infof("retrieving product manifest from [%s]", manifestLoc)
		img := v1alpha1.Image{
			Name: manifestLoc,
		}
		err := storeImage(ctx, s, img, o.Platform, rso, ro)
		if err != nil {
			return err
		}
		err = ExtractCmd(ctx, &flags.ExtractOpts{StoreRootOpts: o.StoreRootOpts}, s, fmt.Sprintf("hauler/%s-manifest.yaml:%s", parts[0], tag))
		if err != nil {
			return err
		}
		filename := fmt.Sprintf("%s-manifest.yaml", parts[0])

		fi, err := os.Open(filename)
		if err != nil {
			return err
		}
		err = processContent(ctx, fi, o, s, rso, ro)
		if err != nil {
			return err
		}
	}

	// if passed a local manifest, process it
	for _, filename := range o.ContentFiles {
		l.Debugf("processing content file: [%s]", filename)
		fi, err := os.Open(filename)
		if err != nil {
			return err
		}
		err = processContent(ctx, fi, o, s, rso, ro)
		if err != nil {
			return err
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
			l.Debugf("skipping sync of unknown content")
			continue
		}

		l.Infof("syncing [%s] to store", obj.GroupVersionKind().String())

		// TODO: Should type switch instead...
		switch obj.GroupVersionKind().Kind {
		case consts.FilesContentKind:
			var cfg v1alpha1.Files
			if err := yaml.Unmarshal(doc, &cfg); err != nil {
				return err
			}

			for _, f := range cfg.Spec.Files {
				err := storeFile(ctx, s, f)
				if err != nil {
					return err
				}
			}

		case consts.ImagesContentKind:
			var cfg v1alpha1.Images
			if err := yaml.Unmarshal(doc, &cfg); err != nil {
				return err
			}
			a := cfg.GetAnnotations()
			for _, i := range cfg.Spec.Images {

				// Check if the user provided a registry.  If a registry is provided in the annotation, use it for the images that don't have a registry in their ref name.
				if a[consts.ImageAnnotationRegistry] != "" || o.Registry != "" {
					newRef, _ := reference.Parse(i.Name)

					newReg := o.Registry // cli flag
					// if no cli flag but there was an annotation, use the annotation.
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

				// Check if the user provided a key.  The flag from the CLI takes precedence over the annotation.  The individual image key takes precedence over both.
				if a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != "" {
					key := o.Key // cli flag
					// if no cli flag but there was an annotation, use the annotation.
					if o.Key == "" && a[consts.ImageAnnotationKey] != "" {
						key, err = homedir.Expand(a[consts.ImageAnnotationKey])
						if err != nil {
							return err
						}
					}
					// the individual image key trumps all
					if i.Key != "" {
						key, err = homedir.Expand(i.Key)
						if err != nil {
							return err
						}
					}
					l.Debugf("key for image [%s]", key)

					// verify signature using the provided key.
					err := cosign.VerifySignature(ctx, s, key, i.Name, rso, ro)
					if err != nil {
						l.Errorf("signature verification failed for image [%s]. ** hauler will skip adding this image to the store **:\n%v", i.Name, err)
						continue
					}
					l.Infof("signature verified for image [%s]", i.Name)
				}

				// Check if the user provided a platform.  The flag from the CLI takes precedence over the annotation.  The individual image platform takes precedence over both.
				platform := o.Platform // cli flag
				// if no cli flag but there was an annotation, use the annotation.
				if o.Platform == "" && a[consts.ImageAnnotationPlatform] != "" {
					platform = a[consts.ImageAnnotationPlatform]
				}
				// the individual image platform trumps all
				if i.Platform != "" {
					platform = i.Platform
				}

				err = storeImage(ctx, s, i, platform, rso, ro)
				if err != nil {
					return err
				}
			}
			// sync with local index
			s.CopyAll(ctx, s.OCI, nil)

		case consts.ChartsContentKind:
			var cfg v1alpha1.Charts
			if err := yaml.Unmarshal(doc, &cfg); err != nil {
				return err
			}

			for _, ch := range cfg.Spec.Charts {
				// TODO: Provide a way to configure syncs
				err := storeChart(ctx, s, ch, &action.ChartPathOptions{})
				if err != nil {
					return err
				}
			}

		case consts.ChartsCollectionKind:
			var cfg v1alpha1.ThickCharts
			if err := yaml.Unmarshal(doc, &cfg); err != nil {
				return err
			}

			for _, cfg := range cfg.Spec.Charts {
				tc, err := tchart.NewThickChart(cfg, &action.ChartPathOptions{
					RepoURL: cfg.RepoURL,
					Version: cfg.Version,
				})
				if err != nil {
					return err
				}

				if _, err := s.AddOCICollection(ctx, tc); err != nil {
					return err
				}
			}

		case consts.ImageTxtsContentKind:
			var cfg v1alpha1.ImageTxts
			if err := yaml.Unmarshal(doc, &cfg); err != nil {
				return err
			}

			for _, cfgIt := range cfg.Spec.ImageTxts {
				it, err := imagetxt.New(cfgIt.Ref,
					imagetxt.WithIncludeSources(cfgIt.Sources.Include...),
					imagetxt.WithExcludeSources(cfgIt.Sources.Exclude...),
				)
				if err != nil {
					return fmt.Errorf("convert ImageTxt %s: %v", cfg.Name, err)
				}

				if _, err := s.AddOCICollection(ctx, it); err != nil {
					return fmt.Errorf("add ImageTxt %s to store: %v", cfg.Name, err)
				}
			}

		default:
			return fmt.Errorf("unrecognized content or collection type: %s", obj.GroupVersionKind().String())
		}
	}
	return nil
}
