package app

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/packager"
)

type driverPackageOpts struct {
	*rootOpts

	// User defined
	kind        string
	version     string
	archivePath string
}

func NewDriverPackageCommand() *cobra.Command {
	o := driverPackageOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:   "package",
		Short: "package driver",
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.kind, "kind", "k3s",
		"Kind of driver to package (k3s or rke2)")
	f.StringVarP(&o.version, "version", "v",  "",
		"Version of driver to package")
	f.StringVarP(&o.archivePath, "archive", "a", "",
		"Path to the resulting packages compressed archive.  If empty, archive will not be created.")

	return cmd
}

func (o *driverPackageOpts) Run() error {
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

	if err = p.AddDriver(ctx, o.kind, o.version); err != nil {
		return err
	}

	if o.archivePath != "" {
		if err = p.Compress(ctx, o.archivePath); err != nil {
			return err
		}
	}

	return nil
}
