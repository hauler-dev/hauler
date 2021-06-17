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
	dir           string
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
		Use:   "build",
		Short: "Build a self contained compressed archive of manifests and images",
		Long: `
Compressed archives created with this command can be extracted and run anywhere the underlying 'driver' can be run.

Archives are built by collecting all the dependencies (images and manifests) required.

Examples:

	# Build a package containing a helm chart with images autodetected from the generated helm chart
	hauler package build -p path/to/helm/chart

	# Build a package, sourcing from multiple manifest sources and additional images not autodetected
	hauler pkg build -p path/to/raw/manifests -p path/to/kustomize -i busybox:latest -i busybox:musl

	# Build a package using a different version of k3s
	hauler p build -p path/to/chart --driver-version "v1.20.6+k3s1"

	# Build a package from a config file (if ./pkg.yaml does not exist, one will be created)
	hauler package build -c ./pkg.yaml
`,
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
	f.StringVarP(&opts.cfgFile, "config", "c", "",
		"path to config file")
	f.StringVar(&opts.dir, "directory", "",
		"Working directory for building package, if empty, an ephemeral temporary directory will be used.  Set this to persist package artifacts between builds.")
	f.StringVarP(&opts.driver, "driver", "d", "k3s",
		"")
	f.StringVar(&opts.driverVersion, "driver-version", "v1.21.1+k3s1",
		"")
	f.StringVar(&opts.fleetVersion, "fleet-version", "v0.3.5",
		"")
	f.StringSliceVarP(&opts.paths, "path", "p", []string{},
		"")
	f.StringSliceVarP(&opts.images, "image", "i", []string{},
		"")

	return cmd
}

func (o *pkgBuildOpts) PreRun() error {
	_, err := os.Stat(o.cfgFile)
	if os.IsNotExist(err) {
		if o.cfgFile == "" {
			return nil
		}

		o.logger.Warnf("Did not find an existing %s, creating one", o.cfgFile)
		p := o.toPackage()

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

	var p v1alpha1.Package
	if o.cfgFile != "" {
		o.logger.Infof("Config file '%s' specified, attempting to load existing package config", o.cfgFile)
		cfgData, err := os.ReadFile(o.cfgFile)
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal(cfgData, &p); err != nil {
			return err
		}

	} else {
		o.logger.Infof("No config file specified, strictly using cli arguments")
		p = o.toPackage()
	}

	var wdir string
	if o.dir != "" {
		if _, err := os.Stat(o.dir); err != nil {
			o.logger.Errorf("Failed to use specified working directory: %s\n%v", err)
			return err
		}

		wdir = o.dir
	} else {
		tmpdir, err := os.MkdirTemp("", "hauler")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpdir)
		wdir = tmpdir
	}

	pkgr := packager.NewPackager(wdir, o.logger)

	d := driver.NewDriver(p.Spec.Driver)
	if _, bErr := pkgr.PackageBundles(ctx, p.Spec.Paths...); bErr != nil {
		return bErr
	}

	if iErr := pkgr.PackageImages(ctx, o.images...); iErr != nil {
		return iErr
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

func (o *pkgBuildOpts) toPackage() v1alpha1.Package {
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
	return p
}
