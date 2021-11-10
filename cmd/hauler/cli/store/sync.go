package store

import (
	"bufio"
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/cache"
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

func SyncCmd(ctx context.Context, o *SyncOpts, s *store.Store, c cache.Cache) error {
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
			gvk, err := content.ValidateType(doc)
			if err != nil {
				return err
			}

			l.Infof("syncing [%s/%s] to [%s]", gvk.APIVersion, gvk.Kind, s.DataDir)

			switch gvk.Kind {
			case v1alpha1.FilesContentKind:
				var cfg v1alpha1.Files
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				for _, f := range cfg.Spec.Files {
					err := storeFile(ctx, s, c, f)
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
					err := storeImage(ctx, s, c, i)
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
					err := storeChart(ctx, s, c, ch)
					if err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}
