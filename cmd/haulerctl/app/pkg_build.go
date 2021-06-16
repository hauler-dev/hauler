package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/yaml"
)

type pkgBuildOpts struct {
	*rootOpts

	cfgFile string

	name          string
	driver        string
	driverVersion string

	fleetVersion string

	images []string
	paths  []string
}

func NewPkgBuildCommand() *cobra.Command {
	opts := pkgBuildOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:     "build",
		Short:   "",
		Long:    "",
		Aliases: []string{"b"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.PersistentFlags()
	f.StringVarP(&opts.name, "name", "n", "pkg",
		"name of the pkg to create, will dicate file name")
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

func (o *pkgBuildOpts) PreRun() error {
	_, err := os.Stat(o.cfgFile)
	if os.IsNotExist(err) {
		o.logger.Warnf("Did not find an existing %s, creating one", o.cfgFile)
		p := v1alpha1.Package{
			TypeMeta: metav1.TypeMeta{
				Kind:       "",
				APIVersion: "",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: o.name,
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
	} else if err != nil {
		return err
	}

	return nil
}

func (o *pkgBuildOpts) Run() error {
	o.logger.Infof("Building package")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfgData, err := os.ReadFile(o.cfgFile)
	if err != nil {
		return err
	}

	var p v1alpha1.Package
	if err := yaml.Unmarshal(cfgData, &p); err != nil {
		return err
	}

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}

	pkgr := packager.NewPackager(tmpdir, o.logger)

	d := driver.NewDriver(p.Spec.Driver)
	if _, bErr := pkgr.PackageBundles(ctx, p.Spec.Paths...); bErr != nil {
		return bErr
	}

	if dErr := pkgr.PackageDriver(ctx, d); dErr != nil {
		return dErr
	}

	if fErr := pkgr.PackageFleet(ctx, p.Spec.Fleet); fErr != nil {
		return fErr
	}

	a := packager.NewArchiver()
	if aErr := pkgr.Archive(a, p, o.name); aErr != nil {
		return aErr
	}

	o.logger.Successf("Finished building package")
	return nil
}
