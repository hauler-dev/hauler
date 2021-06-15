package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/bootstrap"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
	"os"
)

type deployOpts struct {
	*rootOpts

	haulerDir string
}

// NewBootstrapCommand new a new sub command of haulerctl that bootstraps a cluster
func NewBootstrapCommand() *cobra.Command {
	opts := &deployOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Single-command install of a k3s cluster with known tools running inside of it",
		Long: `Single-command install of a k3s cluster with known tools running inside of it. Tools
		include an OCI registry and Git server`,
		Aliases: []string{"b", "boot"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args[0])
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.haulerDir, "hauler-dir", "", "/opt/hauler", "Directory to install hauler components in")

	return cmd
}

// Run performs the operation.
func (o *deployOpts) Run(packagePath string) error {
	o.logger.Infof("Bootstrapping from '%s'", packagePath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)
	o.logger.Debugf("Using temporary working directory: %s", tmpdir)

	a := packager.NewArchiver()
	err = packager.Unpackage(a, packagePath, tmpdir)
	if err != nil {
		return err
	}

	b, err := bootstrap.NewBooter(tmpdir)
	if err != nil {
		return err
	}

	d := driver.NewDriver(b.Package.Spec.Driver)
	if err != nil {
		return err
	}

	o.logger.Infof("Performing pre %s boot steps", b.Package.Spec.Driver.Type)
	if err := b.PreBoot(ctx, d, o.logger); err != nil {
		return err
	}

	o.logger.Infof("Booting %s", b.Package.Spec.Driver.Type)
	if err := b.Boot(ctx, d, o.logger); err != nil {
		return err
	}

	o.logger.Infof("Performing post %s boot steps", b.Package.Spec.Driver.Type)
	if err := b.PostBoot(ctx, d, o.logger); err != nil {
		return err
	}

	o.logger.Infof("Success! You can access the cluster with '/opt/hauler/bin/kubectl'")

	return nil
}
