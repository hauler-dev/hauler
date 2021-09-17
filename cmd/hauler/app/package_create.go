package app

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/packager"
)

type packageCreateOpts struct {
	*rootOpts
	*packageOpts

	// Inputs
	packagePaths []string
	archivePath  string
	driverType   string

	// Generated
	packages []v1alpha1.Package
}

func NewBundleCreateCommand() *cobra.Command {
	opts := &packageCreateOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"c"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.driverType, "driver", "d", "k3s", "Driver to use when creating the package (none, k3s, rke2)")
	f.StringVarP(&opts.archivePath, "archive", "a", "", "Path to the resulting packages compressed archive.  If empty, archive will not be created.")
	f.StringArrayVarP(&opts.packagePaths, "packages", "p", []string{}, "Path to hauler package(s), can be specified multiple times.")

	return cmd
}

func (o *packageCreateOpts) PreRun() error {
	for _, bpath := range o.packagePaths {
		bundle, err := loadPackage(bpath)
		if err != nil {
			return err
		}
		o.packages = append(o.packages, bundle)
	}

	if o.driverType == "none" {
		// Convert between human friendly and nil type
		o.driverType = ""
	}

	return nil
}

func loadPackage(path string) (v1alpha1.Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return v1alpha1.Package{}, err
	}

	var bundle v1alpha1.Package
	if err := yaml.Unmarshal(data, &bundle); err != nil {
		return v1alpha1.Package{}, err
	}

	return bundle, nil
}

func (o *packageCreateOpts) Run() error {
	logger := o.rootOpts.logger

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := o.getStore(ctx, "")
	if err != nil {
		return err
	}

	p, err := packager.NewPackager(s, logger)
	if err != nil {
		return err
	}

	for i, pkg := range o.packages {
		logger.Infof("Creating package '%s' from %s", pkg.Name, o.packagePaths[i])
		if err = p.Create(ctx, o.driverType, pkg); err != nil {
			return err
		}
	}

	if o.archivePath != "" {
		logger.Infof("Archiving and compressing bundle to %s", o.archivePath)
		if err = p.Compress(ctx, o.archivePath); err != nil {
			return err
		}
	}

	return nil
}
