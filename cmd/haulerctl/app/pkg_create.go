package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/yaml"
)

type pkgCreateOpts struct {
	cfgFile string

	driver        string
	driverVersion string

	fleetVersion string

	images []string
	paths  []string
}

func NewPkgCreateCommand() *cobra.Command {
	opts := pkgCreateOpts{}

	cmd := &cobra.Command{
		Use:     "create",
		Short:   "",
		Long:    "",
		Aliases: []string{"c"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.PersistentFlags()
	f.StringVarP(&opts.cfgFile, "config", "c", "./pkg.yaml",
		"path to config file")
	f.StringVarP(&opts.driver, "driver", "d", "k3s",
		"")
	f.StringVar(&opts.driverVersion, "driver-version", "v1.21.1+k3s1",
		"")
	f.StringVar(&opts.fleetVersion, "fleet-version", "v0.3.5",
		"")
	f.StringSliceVarP(&opts.images, "image", "i", []string{},
		"")
	f.StringSliceVarP(&opts.paths, "path", "p", []string{},
		"")

	return cmd
}

func (o *pkgCreateOpts) PreRun() error {
	_, err := os.Stat(o.cfgFile)
	if os.IsNotExist(err) {
		logrus.Infof("Could not find %s, creating one", o.cfgFile)
		p := v1alpha1.Package{
			TypeMeta: metav1.TypeMeta{
				Kind:       "",
				APIVersion: "",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
			},
			Spec: v1alpha1.PackageSpec{
				Fleet: v1alpha1.Fleet{
					Version: o.fleetVersion,
				},
				Driver: v1alpha1.Driver{
					Type:    o.driver,
					Version: o.driverVersion,
				},
				Paths:  o.paths,
				Images: o.images,
			},
		}

		data, err := yaml.Marshal(p)
		if err != nil {
			return err
		}

		if err := os.WriteFile(o.cfgFile, data, 0644); err != nil {
			return err
		}
	}

	if err != nil {
		return err
	}
	return nil
}

func (o *pkgCreateOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = ctx

	return nil
}
