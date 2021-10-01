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

	// Generated
	packages []v1alpha1.Package
}

func NewPackageCreateCommand() *cobra.Command {
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
	f.StringVarP(&opts.archivePath, "archive", "a", "",
		"Path to the resulting packages compressed archive.  If empty, archive will not be created.")
	f.StringArrayVarP(&opts.packagePaths, "packages", "p", []string{},
		"Path to hauler package(s), can be specified multiple times.")

	return cmd
}

func (o *packageCreateOpts) PreRun() error {
	for _, ppath := range o.packagePaths {
		pkg, err := loadPackage(ppath)
		if err != nil {
			return err
		}
		o.packages = append(o.packages, pkg)
	}

	return nil
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

	for _, pkg := range o.packages {
		if err = p.AddPackage(ctx, pkg); err != nil {
			return err
		}
	}

	if o.archivePath != "" {
		if err = p.Compress(ctx, o.archivePath); err != nil {
			return err
		}
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
