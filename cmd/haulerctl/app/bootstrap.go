package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/bootstrap"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

type deployOpts struct {
	*rootOpts

	haulerDir string
}

// NewBootstrapCommand new a new sub command of haulerctl that bootstraps a cluster
func NewBootstrapCommand() *cobra.Command {
	opts := &deployOpts{
		rootOpts:  &ro,
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

	_, tmpdir, err := tmpFS()
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)
	o.logger.Debugf("Using temporary working directory: %s", tmpdir)

	z := newTarZstd()

	err = z.Unarchive(packagePath, tmpdir)
	if err != nil {
		return err
	}

	bundleData, err := os.ReadFile(filepath.Join(tmpdir, "package.json"))
	if err != nil {
		return err
	}

	var p v1alpha1.Package
	if err := yaml.Unmarshal(bundleData, &p); err != nil {
		return err
	}

	b, err := bootstrap.NewBooter(tmpdir)
	if err != nil {
		return err
	}

	o.logger.Infof("Initializing package for driver: %s", p.Spec.Driver.Kind)
	if err := b.Init(); err != nil {
		return err
	}

	o.logger.Infof("Performing pre %s boot steps", p.Spec.Driver.Kind)

	o.logger.Infof("Booting %s", p.Spec.Driver.Kind)

	o.logger.Infof("Performing post %s boot steps", p.Spec.Driver.Kind)

	o.logger.Infof("Success!")

	_ = ctx

	return nil
}
