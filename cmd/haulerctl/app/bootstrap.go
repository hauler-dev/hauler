package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/bundle/boot"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"os"
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
func (o *deployOpts) Run(bootPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}

	tp := bundle.Path(tmpdir)

	if err := packager.Decompress(bootPath, tmpdir); err != nil {
		return err
	}

	_, err = os.Stat(tp.Path("boot.bundle.yaml"))
	if err != nil {
		return err
	}

	data, err := os.ReadFile(tp.Path("boot.bundle.yaml"))
	if err != nil {
		return err
	}

	var b *boot.Bundle
	err = yaml.Unmarshal(data, &b)
	if err != nil {
		return err
	}

	err := b.Install(tp.Path())
	if err != nil {
		return err
	}

	return nil
}
