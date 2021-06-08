package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/bootstrap"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

type deployOpts struct {
	haulerDir string
}

// NewBootstrapCommand new a new sub command of haulerctl that bootstraps a cluster
func NewBootstrapCommand() *cobra.Command {
	opts := &deployOpts{}

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Single-command install of a k3s cluster with known tools running inside of it",
		Long: `Single-command install of a k3s cluster with known tools running inside of it. Tools
		include an OCI registry and Git server`,
		Aliases: []string{"b"},
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fsys, tmpdir, err := tmpFS()
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)
	logrus.Infof("Built temporary working dir: %s", tmpdir)

	z := newTarZstd()

	logrus.Infof("Unarchiving %s", packagePath)
	err = z.Unarchive(packagePath, tmpdir)
	if err != nil {
		return err
	}
	logrus.Infof("Unarchived %s to %s", packagePath, tmpdir)

	logrus.Infof("Loading package.json from archive")
	bundleData, err := os.ReadFile(filepath.Join(tmpdir, "package.json"))
	if err != nil {
		return err
	}

	var p v1alpha1.Package
	if err := yaml.Unmarshal(bundleData, &p); err != nil {
		return err
	}
	logrus.Infof("Loaded package '%s'", p.Name)

	logrus.Infof("Bootstrapping cluster")
	err = bootstrap.Boot(ctx, p, fsys)
	if err != nil {
		return err
	}

	return nil
}
