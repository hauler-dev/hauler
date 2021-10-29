package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/get"
)

func addGet(parent *cobra.Command) {
	o := &get.Opts{}

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get OCI content from a registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, arg []string) error {
			ctx := cmd.Context()

			return get.Cmd(ctx, o, arg[0])
		},
	}
	o.AddArgs(cmd)

	parent.AddCommand(cmd)
}
