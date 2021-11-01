package store

import (
	"bufio"
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
	"github.com/rancherfederal/hauler/pkg/content/k3s"
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

	s.Open()
	defer s.Close()

	for _, filename := range o.ContentFiles {
		l.Debugf("Syncing content file: '%s'", filename)
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

			l.Infof("Syncing content from: '%s'", gvk.String())

			switch gvk.Kind {
			case v1alpha1.FilesContentKind:
				var cfg v1alpha1.Files
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				for _, f := range cfg.Spec.Files {
					oci := file.NewFile(f)
					if err := s.Add(ctx, oci); err != nil {
						return err
					}
				}

			case v1alpha1.ImagesContentKind:
				var cfg v1alpha1.Images
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				for _, i := range cfg.Spec.Images {
					oci := image.NewImage(i)

					if err := s.Add(ctx, oci); err != nil {
						return err
					}
				}

			case v1alpha1.ChartsContentKind:
				var cfg v1alpha1.Charts
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				for _, c := range cfg.Spec.Charts {
					oci := chart.NewChart(c)
					if err := s.Add(ctx, oci); err != nil {
						return err
					}
				}

			case v1alpha1.DriverContentKind:
				var cfg v1alpha1.Driver
				if err := yaml.Unmarshal(doc, &cfg); err != nil {
					return err
				}

				oci, err := k3s.NewK3s(cfg.Spec.Version)
				if err != nil {
					return err
				}

				if err := s.Add(ctx, oci); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
