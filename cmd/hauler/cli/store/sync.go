package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/ocil/pkg/store"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha2"
	tchart "github.com/rancherfederal/hauler/pkg/collection/chart"
	"github.com/rancherfederal/hauler/pkg/collection/imagetxt"
	"github.com/rancherfederal/hauler/pkg/collection/k3s"
	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/log"
)

type SyncOpts struct {
	*RootOpts
	ContentFiles []string
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.ContentFiles, "files", "f", []string{}, "Path to content files")
}

func SyncCmd(ctx context.Context, o *SyncOpts, s *store.Layout) error {
	l := log.FromContext(ctx)

	// Start from an empty store (contents are cached elsewhere)
	l.Debugf("flushing content store")
	if err := s.Flush(ctx); err != nil {
		return err
	}

	for _, filename := range o.ContentFiles {
		l.Debugf("processing content file: '%s'", filename)
		fi, err := os.Open(filename)
		if err != nil {
			return err
		}

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

			gvk := obj.GroupVersionKind()
			switch {
			// content.hauler.cattle.io/v1alpha1
			// collection.hauler.cattle.io/v1alpha1
			case gvk.Version == v1alpha1.Version:
				if gvk.GroupVersion() == v1alpha1.ContentGroupVersion || gvk.GroupVersion() == v1alpha1.CollectionGroupVersion {
					return fmt.Errorf(
						"%s is deprecated and unsupported; use %s instead",
						gvk.GroupVersion().String(),
						schema.GroupVersion{Group: gvk.Group, Version: v1alpha2.Version}.String(),
					)
				}
				return fmt.Errorf("unrecognized API object type: %s", gvk.String())
			// content.hauler.cattle.io/v1alpha2
			case gvk.GroupVersion() == v1alpha2.ContentGroupVersion:
				switch gvk.Kind {
				// content.hauler.cattle.io/v1alpha2 Files
				case v1alpha2.FilesContentKind:
					var cfg v1alpha2.Files
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					for _, f := range cfg.Spec.Files {
						err := storeFile(ctx, s, f)
						if err != nil {
							return err
						}
					}

				// content.hauler.cattle.io/v1alpha2 Images
				case v1alpha2.ImagesContentKind:
					var cfg v1alpha2.Images
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					for _, i := range cfg.Spec.Images {
						err := storeImage(ctx, s, i)
						if err != nil {
							return err
						}
					}

				// content.hauler.cattle.io/v1alpha2 Charts
				case v1alpha2.ChartsContentKind:
					var cfg v1alpha2.Charts
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

				// content.hauler.cattle.io/v1alpha2 unknown Kind
				default:
					return fmt.Errorf("unrecognized content kind: %s", gvk.Kind)
				}

			// collection.hauler.cattle.io/v1alpha2
			case gvk.GroupVersion() == v1alpha2.CollectionGroupVersion:
				switch gvk.Kind {
				// collection.hauler.cattle.io/v1alpha2 K3s
				case v1alpha2.K3sCollectionKind:
					var cfg v1alpha2.K3s
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					k, err := k3s.NewK3s(cfg.Spec.Version)
					if err != nil {
						return err
					}
					if _, err := s.AddOCICollection(ctx, k); err != nil {
						return err
					}

				// collection.hauler.cattle.io/v1alpha2 ThickCharts
				case v1alpha2.ChartsCollectionKind:
					var cfg v1alpha2.ThickCharts
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

				// collection.hauler.cattle.io/v1alpha2 ImageTxts
				case v1alpha2.ImageTxtsContentKind:
					var cfg v1alpha2.ImageTxts
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					for _, cfgIt := range cfg.Spec.ImageTxts {
						it, err := imagetxt.New(cfgIt.Path,
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

				// collection.hauler.cattle.io/v1alpha2 unknown Kind
				default:
					return fmt.Errorf("unrecognized collection kind: %s", gvk.Kind)
				}
			// unknown API group + version
			default:
				return fmt.Errorf("unrecognized API object type: %s", gvk)
			}
		}
	}

	return nil
}
