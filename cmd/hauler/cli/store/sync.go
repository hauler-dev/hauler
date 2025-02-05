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
	convert "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/convert"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	v1alpha1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
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
		l.Infof("processing content for product [%s]", product)
		parts := strings.Split(product, "=")
		tag := strings.ReplaceAll(parts[1], "+", "-")

		ProductRegistry := o.ProductRegistry // cli flag
		// if no cli flag use CarbideRegistry.
		if o.ProductRegistry == "" {
			ProductRegistry = consts.CarbideRegistry
		}

		manifestLoc := fmt.Sprintf("%s/hauler/%s-manifest.yaml:%s", ProductRegistry, parts[0], tag)
		l.Infof("retrieving product manifest from [%s]", manifestLoc)
		img := v1.Image{
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
		l.Infof("processing content [%s]", filename)
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
			l.Warnf("skipping syncing due to %v", err)
			continue
		}

		gvk := obj.GroupVersionKind()
		l.Infof("syncing content [%s] with [kind=%s] to store [%s]", gvk.GroupVersion(), gvk.Kind, o.StoreDir)

		switch gvk.Kind {

		case consts.FilesContentKind:
			switch gvk.Version {
			case "v1alpha1":
				l.Warnf("!!! DEPRECATION WARNING !!! apiVersion [%s] will be removed in a future release !!! DEPRECATION WARNING !!!", gvk.Version)

				var alphaCfg v1alpha1.Files
				if err := yaml.Unmarshal(doc, &alphaCfg); err != nil {
					return err
				}
				var v1Cfg v1.Files
				if err := convert.ConvertFiles(&alphaCfg, &v1Cfg); err != nil {
					return err
				}
				for _, f := range v1Cfg.Spec.Files {
					if err := storeFile(ctx, s, f); err != nil {
						return err
					}
				}

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
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1 and v1alpha1]", gvk.Version, gvk.Kind)
			}

		case consts.ImagesContentKind:
			switch gvk.Version {
			case "v1alpha1":
				l.Warnf("!!! DEPRECATION WARNING !!! apiVersion [%s] will be removed in a future release !!! DEPRECATION WARNING !!!", gvk.Version)

				var alphaCfg v1alpha1.Images
				if err := yaml.Unmarshal(doc, &alphaCfg); err != nil {
					return err
				}
				var v1Cfg v1.Images
				if err := convert.ConvertImages(&alphaCfg, &v1Cfg); err != nil {
					return err
				}

				a := v1Cfg.GetAnnotations()
				for _, i := range v1Cfg.Spec.Images {

					if a[consts.ImageAnnotationRegistry] != "" || o.Registry != "" {
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

					if a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != "" {
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

						if err := cosign.VerifySignature(ctx, s, key, i.Name, rso, ro); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("signature verified for image [%s]", i.Name)
					}

					platform := o.Platform
					if o.Platform == "" && a[consts.ImageAnnotationPlatform] != "" {
						platform = a[consts.ImageAnnotationPlatform]
					}
					if i.Platform != "" {
						platform = i.Platform
					}

					if err := storeImage(ctx, s, i, platform, rso, ro); err != nil {
						return err
					}
				}
				s.CopyAll(ctx, s.OCI, nil)

			case "v1":
				var cfg v1.Images
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				a := cfg.GetAnnotations()
				for _, i := range cfg.Spec.Images {

					if a[consts.ImageAnnotationRegistry] != "" || o.Registry != "" {
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

					if a[consts.ImageAnnotationKey] != "" || o.Key != "" || i.Key != "" {
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

						if err := cosign.VerifySignature(ctx, s, key, i.Name, rso, ro); err != nil {
							l.Errorf("signature verification failed for image [%s]... skipping...\n%v", i.Name, err)
							continue
						}
						l.Infof("signature verified for image [%s]", i.Name)
					}

					platform := o.Platform
					if o.Platform == "" && a[consts.ImageAnnotationPlatform] != "" {
						platform = a[consts.ImageAnnotationPlatform]
					}
					if i.Platform != "" {
						platform = i.Platform
					}

					if err := storeImage(ctx, s, i, platform, rso, ro); err != nil {
						return err
					}
				}
				s.CopyAll(ctx, s.OCI, nil)

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1 and v1alpha1]", gvk.Version, gvk.Kind)
			}

		case consts.ChartsContentKind:
			switch gvk.Version {
			case "v1alpha1":
				l.Warnf("!!! DEPRECATION WARNING !!! apiVersion [%s] will be removed in a future release !!! DEPRECATION WARNING !!!", gvk.Version)

				var alphaCfg v1alpha1.Charts
				if err := yaml.Unmarshal(doc, &alphaCfg); err != nil {
					return err
				}
				var v1Cfg v1.Charts
				if err := convert.ConvertCharts(&alphaCfg, &v1Cfg); err != nil {
					return err
				}
				for _, ch := range v1Cfg.Spec.Charts {
					if err := storeChart(ctx, s, ch, &action.ChartPathOptions{}); err != nil {
						return err
					}
				}

			case "v1":
				var cfg v1.Charts
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				for _, ch := range cfg.Spec.Charts {
					if err := storeChart(ctx, s, ch, &action.ChartPathOptions{}); err != nil {
						return err
					}
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1 and v1alpha1]", gvk.Version, gvk.Kind)
			}

		case consts.ChartsCollectionKind:
			switch gvk.Version {
			case "v1alpha1":
				l.Warnf("!!! DEPRECATION WARNING !!! apiVersion [%s] will be removed in a future release !!! DEPRECATION WARNING !!!", gvk.Version)

				var alphaCfg v1alpha1.ThickCharts
				if err := yaml.Unmarshal(doc, &alphaCfg); err != nil {
					return err
				}
				var v1Cfg v1.ThickCharts
				if err := convert.ConvertThickCharts(&alphaCfg, &v1Cfg); err != nil {
					return err
				}
				for _, chObj := range v1Cfg.Spec.Charts {
					tc, err := tchart.NewThickChart(chObj, &action.ChartPathOptions{
						RepoURL: chObj.RepoURL,
						Version: chObj.Version,
					})
					if err != nil {
						return err
					}
					if _, err := s.AddOCICollection(ctx, tc); err != nil {
						return err
					}
				}

			case "v1":
				var cfg v1.ThickCharts
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}
				for _, chObj := range cfg.Spec.Charts {
					tc, err := tchart.NewThickChart(chObj, &action.ChartPathOptions{
						RepoURL: chObj.RepoURL,
						Version: chObj.Version,
					})
					if err != nil {
						return err
					}
					if _, err := s.AddOCICollection(ctx, tc); err != nil {
						return err
					}
				}

			default:
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1 and v1alpha1]", gvk.Version, gvk.Kind)
			}

		case consts.ImageTxtsContentKind:
			switch gvk.Version {
			case "v1alpha1":
				l.Warnf("!!! DEPRECATION WARNING !!! apiVersion [%s] will be removed in a future release !!! DEPRECATION WARNING !!!", gvk.Version)

				var alphaCfg v1alpha1.ImageTxts
				if err := yaml.Unmarshal(doc, &alphaCfg); err != nil {
					return err
				}
				var v1Cfg v1.ImageTxts
				if err := convert.ConvertImageTxts(&alphaCfg, &v1Cfg); err != nil {
					return err
				}
				for _, cfgIt := range v1Cfg.Spec.ImageTxts {
					it, err := imagetxt.New(cfgIt.Ref,
						imagetxt.WithIncludeSources(cfgIt.Sources.Include...),
						imagetxt.WithExcludeSources(cfgIt.Sources.Exclude...),
					)
					if err != nil {
						return fmt.Errorf("convert ImageTxt %s: %v", v1Cfg.Name, err)
					}
					if _, err := s.AddOCICollection(ctx, it); err != nil {
						return fmt.Errorf("add ImageTxt %s to store: %v", v1Cfg.Name, err)
					}
				}

			case "v1":
				var cfg v1.ImageTxts
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
				return fmt.Errorf("unsupported version [%s] for kind [%s]... valid versions are [v1 and v1alpha1]", gvk.Version, gvk.Kind)
			}

		default:
			return fmt.Errorf("unsupported kind [%s]... valid kinds are [Files, Images, Charts, ThickCharts, ImageTxts]", gvk.Kind)
		}
	}
	return nil
}
