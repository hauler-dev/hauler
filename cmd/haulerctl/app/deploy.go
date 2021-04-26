package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/deployer"
	"github.com/spf13/cobra"
)

type deployOpts struct {
	haul string
}

func NewDeployCommand() *cobra.Command {
	opts := &deployOpts{}

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "deploy all dependencies from a generated package",
		Long: `deploy all dependencies from a generated package.

Given an archive generated from the package command, deploy all needed
components to serve packaged dependencies.`,
		Aliases: []string{"d", "dpl", "dep"},
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args[0])
		},
	}

	return cmd
}

// Run performs the operation.
func (o *deployOpts) Run(haul string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dpl := deployer.NewDeployer()
	if err := dpl.Deploy(ctx, haul); err != nil {
		return err
	}

	return nil
}
