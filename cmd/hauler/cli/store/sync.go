package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha2"
	tchart "github.com/rancherfederal/hauler/pkg/collection/chart"
	"github.com/rancherfederal/hauler/pkg/collection/k3s"
	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type SyncOpts struct {
	ContentFiles []string
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.ContentFiles, "files", "f", []string{}, "Path to content files")
}

func SyncCmd(ctx context.Context, o *SyncOpts, s *store.Store) error {
	l := log.FromContext(ctx)

	// Start from an empty store (contents are cached elsewhere)
	l.Debugf("flushing any existing content in store: %s", s.DataDir)
	if err := s.Flush(ctx); err != nil {
		return err
	}

	s.Open()
	defer s.Close()

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
				return err
			}

			l.Infof("syncing [%s] to [%s]", obj.GroupVersionKind().String(), s.DataDir)

			switch obj.GroupVersionKind().GroupVersion() {
			// content.hauler.cattle.io/v1alpha1
			case schema.GroupVersion{Group: v1alpha1.ContentGroup, Version: v1alpha1.Version}:
				l.Warnf(
					"API version %s is deprecated; %s is recommended instead",
					obj.GroupVersionKind().GroupVersion().String(),
					schema.GroupVersion{Group: v1alpha2.ContentGroup, Version: v1alpha2.Version}.String(),
				)
				switch obj.GroupVersionKind().Kind {
				// content.hauler.cattle.io/v1alpha1 Files
				case v1alpha1.FilesContentKind:
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
				// content.hauler.cattle.io/v1alpha1 Images
				case v1alpha1.ImagesContentKind:
					var cfg v1alpha1.Images
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					for _, i := range cfg.Spec.Images {
						err := storeImage(ctx, s, i)
						if err != nil {
							return err
						}
					}
				// content.hauler.cattle.io/v1alpha1 Charts
				case v1alpha1.ChartsContentKind:
					var cfg v1alpha1.Charts
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					for _, ch := range cfg.Spec.Charts {
						err := storeChart(ctx, s, ch)
						if err != nil {
							return err
						}
					}
				// collection.hauler.cattle.io/v1alpha1 unknown
				default:
					return fmt.Errorf("unsupported Kind %s for %s", obj.GroupVersionKind().Kind, obj.GroupVersionKind().GroupVersion().String())
				}
			// collection.hauler.cattle.io/v1alpha1
			case schema.GroupVersion{Group: v1alpha1.CollectionGroup, Version: v1alpha1.Version}:
				l.Warnf(
					"API version %s is deprecated; %s is recommended instead",
					obj.GroupVersionKind().GroupVersion().String(),
					schema.GroupVersion{Group: v1alpha2.CollectionGroup, Version: v1alpha2.Version}.String(),
				)
				switch obj.GroupVersionKind().Kind {
				// collection.hauler.cattle.io/v1alpha1 K3s
				case v1alpha1.K3sCollectionKind:
					var cfg v1alpha1.K3s
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					k, err := k3s.NewK3s(cfg.Spec.Version)
					if err != nil {
						return err
					}
					if _, err := s.AddCollection(ctx, k); err != nil {
						return err
					}
				// collection.hauler.cattle.io/v1alpha1 ThickCharts
				case v1alpha1.ChartsCollectionKind:
					var cfg v1alpha1.ThickCharts
					if err := yaml.Unmarshal(doc, &cfg); err != nil {
						return err
					}
					for _, cfg := range cfg.Spec.Charts {
						tc, err := tchart.NewChart(cfg.Name, cfg.RepoURL, cfg.Version)
						if err != nil {
							return err
						}
						if _, err := s.AddCollection(ctx, tc); err != nil {
							return err
						}
					}
				// collection.hauler.cattle.io/v1alpha1 unknown
				default:
					return fmt.Errorf("unsupported Kind %s for %s", obj.GroupVersionKind().Kind, obj.GroupVersionKind().GroupVersion().String())
				}
			// content.hauler.cattle.io/v1alpha2
			case schema.GroupVersion{Group: v1alpha2.ContentGroup, Version: v1alpha2.Version}:
			// collection.hauler.cattle.io/v1alpha2
			case schema.GroupVersion{Group: v1alpha2.CollectionGroup, Version: v1alpha2.Version}:
			// unknown
			default:
				return fmt.Errorf("unrecognized content/collection type: %s", obj.GroupVersionKind().String())
			}
		}
	}

	return nil
}
