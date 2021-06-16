package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/bootstrap"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
	"os"
)

type pkgRunOpts struct {
	*rootOpts

	cfgFile string
}

func NewPkgRunCommand() *cobra.Command {
	opts := pkgRunOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:     "run",
		Short:   "",
		Long:    "",
		Aliases: []string{"r"},
		Args:    cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args[0])
		},
	}

	return cmd
}

func (o *pkgRunOpts) PreRun() error {
	return nil
}

func (o *pkgRunOpts) Run(pkgPath string) error {
	o.logger.Infof("Running from '%s'", pkgPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	o.logger.Debugf("Using temporary working directory: %s", tmpdir)

	a := packager.NewArchiver()

	if err := packager.Unpackage(a, pkgPath, tmpdir); err != nil {
		return err
	}
	o.logger.Debugf("Unpackaged %s", pkgPath)

	b, err := bootstrap.NewBooter(tmpdir, o.logger)
	if err != nil {
		return err
	}

	d := driver.NewDriver(b.Package.Spec.Driver)

	if preErr := b.PreBoot(ctx, d); preErr != nil {
		return preErr
	}

	if bErr := b.Boot(ctx, d); bErr != nil {
		return bErr
	}

	if postErr := b.PostBoot(ctx, d); postErr != nil {
		return postErr
	}

	o.logger.Successf("Access the cluster with '/opt/hauler/bin/kubectl'")
	return nil
}
