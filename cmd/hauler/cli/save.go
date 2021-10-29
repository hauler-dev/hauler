package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/save"
)

func addSave(parent *cobra.Command) {
	o := &save.Opts{}

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save hauler's store into a transportable compressed archive",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return save.Cmd(ctx, o, o.FileName, s.DataDir)
		},
	}
	o.AddArgs(cmd)

	parent.AddCommand(cmd)
}
