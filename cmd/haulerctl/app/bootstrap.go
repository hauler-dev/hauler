package app

import (
	"context"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/bootstrap"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

type deployOpts struct {
	haulerDir string
}

// NewBootstrapCommand create a new sub command of haulerctl that bootstraps a cluster
func NewBootstrapCommand() *cobra.Command {
	opts := &deployOpts{}

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Single-command install of a k3s cluster with known tools running inside of it",
		Long: `Single-command install of a k3s cluster with known tools running inside of it. Tools
		include an OCI registry and Git server`,
		Aliases: []string{"b", "btstrp"},
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
func (o *deployOpts) Run(haulPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	z := archiver.NewTarZstd()
	z.OverwriteExisting = true
	z.MkdirAll = true

	err := z.Unarchive(haulPath, o.haulerDir)
	if err != nil {
		return err
	}

	haulerCfgPath := filepath.Join(o.haulerDir, "hauler.yaml")
	data, err := os.ReadFile(haulerCfgPath)

	var h haul.Haul
	if err := yaml.Unmarshal(data, &h); err != nil {
		return err
	}

	bstrp := bootstrap.NewBootstrapper(h, o.haulerDir)
	err = bstrp.Bootstrap(ctx)
	if err != nil {
		return err
	}

	return nil
}
