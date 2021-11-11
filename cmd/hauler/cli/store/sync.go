package store

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/collection/chart"
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
	l.Debugf("running cli command `hauler store sync`")

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

			// TODO: Should type switch instead...
			switch obj.GroupVersionKind().Kind {
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

			case v1alpha1.ChartsCollectionKind:
				var cfg v1alpha1.ThickCharts
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				for _, cfg := range cfg.Spec.Charts {
					tc, err := chart.NewChart(cfg.Name, cfg.RepoURL, cfg.Version)
					if err != nil {
						return err
					}

					if _, err := s.AddCollection(ctx, tc); err != nil {
						return err
					}
				}

			default:
				return fmt.Errorf("unrecognized content/collection type: %s", obj.GroupVersionKind().String())
			}
		}
	}

	return nil
}
