package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/load"
)

func addLoad(parent *cobra.Command) {
	o := &load.Opts{}

	cmd := &cobra.Command{
		Use:  "load",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return load.Cmd(ctx, o, s.DataDir, args...)
		},
	}

	parent.AddCommand(cmd)
}
