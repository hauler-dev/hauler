package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/bundle/boot"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

type bundleBootOpts struct {
	bundleOpts bundleOpts

	name string
	save  bool
	path string
	skipDriver bool
	images []string
	config string
	driverType string
	driverVersion string
}

func NewBundleBootCommand() *cobra.Command {
	opts := &bundleBootOpts{}

	cmd := &cobra.Command{
		Use: "boot",
		Short: "does something",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.name, "name", "n", "boot",
		"Name of the bundle to new")
	f.StringVarP(&opts.config, "config", "c", "boot.bundle.yaml",
		"Name of the config file to use for bundling")
	f.StringVar(&opts.driverType, "driver-type",  "k3s",
		"Type of driver to use for the boot bundle (k3s or rke2)")
	f.StringVar(&opts.driverVersion, "driver-version", "v1.21.1+k3s1",
		"Version of the driver to use, must match appropriately with driver-type")
	f.StringSliceVarP(&opts.images, "images", "i", []string{},
		"Images to include in bundle, can be specified multiple times")
	f.BoolVarP(&opts.save, "save", "s", false,
		"Save bundle")

	return cmd
}

func (o *bundleBootOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bundlePath := filepath.Join(o.path, o.name)

	logrus.Infof("loading boot bundle: %s", bundlePath)
	b, err := o.load(bundlePath)
	if err != nil {
		return err
	}

	err = b.Sync(ctx, bundlePath)
	if err != nil {
		return err
	}

	if o.save {
		logrus.Infof("Exporting bundle to compressed archive")
		//TODO: This is lazy
		err := packager.Export(b, bundlePath, b.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *bundleBootOpts) load(path string) (*boot.Bundle, error) {
	var b *boot.Bundle

	p := bundle.Path(path)
	_, err := os.Stat(p.Path(o.config))
	if os.IsNotExist(err) {
		b = o.newDefault()
		data, _ := yaml.Marshal(b)
		if err := p.WriteFile(o.config, data, 0644); err != nil {
			return nil, err
		}

		//Make the dir structure if we're starting from scratch
		if err = os.MkdirAll(p.Path("bin"), os.ModePerm); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(p.Path("images"), os.ModePerm); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(p.Path("manifests"), os.ModePerm); err != nil {
			return nil, err
		}
		if err = os.MkdirAll(p.Path("charts"), os.ModePerm); err != nil {
			return nil, err
		}
	}

	data, err := os.ReadFile(p.Path(o.config))
	err = yaml.Unmarshal(data, &b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (o *bundleBootOpts) newDefault() *boot.Bundle {
	return &boot.Bundle{
		Name:   "hauler",
		Images: o.images,
		Driver: boot.K3sDriver{ Version: o.driverVersion },

		//TODO: Chart support, maybe specify list of "repo/chart"?
		Charts: []string{},
	}
}