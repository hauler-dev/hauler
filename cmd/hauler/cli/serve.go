package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/serve"
)

func addServe(parent *cobra.Command) {
	o := &serve.Opts{}

	cmd := &cobra.Command{
		Use: "serve",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return serve.Cmd(ctx, o, s.DataDir)
		},
	}
	o.AddFlags(cmd)

	parent.AddCommand(cmd)
}
