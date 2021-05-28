package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

type deployOpts struct {
	haul string
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

	return cmd
}

// Run performs the operation.
func (o *deployOpts) Run(haul string) error {
	fmt.Println("Bootstrap")
	return nil
}
