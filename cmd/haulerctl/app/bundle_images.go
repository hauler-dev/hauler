package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/bundle/image"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

type bundleImagesOpts struct {
	bundleOpts bundleOpts

	name string
	save bool
	path string
	images []string
	config string
}

// NewBundleImagesCommand creates a new sub command of bundle for images
func NewBundleImagesCommand() *cobra.Command {

	opts := &bundleImagesOpts{}

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Download a list of container images, new artifact containing all of them",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.bundleOpts.bundleDir = viper.GetString("bundledir")
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.name, "name", "n", "hauler",
		"Name of the bundle to new")
	f.StringVarP(&opts.config, "config", "c", "image.bundle.yaml",
		"Name of the config file to use for bundling")
	f.StringVarP(&opts.path, "path", "p", "",
		"OCILayoutName to an existing directory to create a bundle from")
	f.BoolVarP(&opts.save, "save", "s", false,
		"Save bundle")
	f.StringSliceVarP(&opts.images, "image", "i", []string{},
		"image to append to layout, can be specified multiple times")

	return cmd
}

func (o *bundleImagesOpts) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	bundlePath := filepath.Join(o.path, o.name)

	logrus.Infof("loading image bundle: %s", o.name)
	b, err := o.load(bundlePath)
	if err != nil {
		return err
	}

	err = b.Sync(ctx, bundlePath)
	if err != nil {
		return err
	}

	return nil
}

func (o *bundleImagesOpts) load(path string) (*image.Bundle, error) {
	var i *image.Bundle

	p := bundle.Path(path)
	_, err := os.Stat(p.Path(o.config))
	if os.IsNotExist(err) {
		//	Create a new default bundle
		i = o.newDefault()
		data, _ := yaml.Marshal(i)
		if err := p.WriteFile(o.config, data, 0644); err != nil {
			return &image.Bundle{}, err
		}
	}

	data, err := os.ReadFile(p.Path(o.config))
	err = yaml.Unmarshal(data, &i)
	if err != nil {
		return &image.Bundle{}, err
	}

	return i, nil
}

func (o *bundleImagesOpts) newDefault() *image.Bundle {
	return &image.Bundle{
		Name:   "hauler",
		Images: o.images,
	}
}