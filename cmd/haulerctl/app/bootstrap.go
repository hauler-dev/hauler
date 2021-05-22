package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

type deployOpts struct {
	haul string
}

func NewBootstrapCommand() *cobra.Command {
	opts := &deployOpts{}

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "deploy all dependencies from a generated package",
		Long: `deploy all dependencies from a generated package.
Given an archive generated from the package command, deploy all needed
components to serve packaged dependencies.`,
		Aliases: []string{"d", "dpl", "dep"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args[0])
		},
	}

	return cmd
}

// Run performs the operation.
func (o *deployOpts) Run(haul string) error {
	fmt.Println("Bootstrap")
	return nil
}
