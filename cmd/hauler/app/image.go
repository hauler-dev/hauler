package app

import "github.com/spf13/cobra"

func NewImageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "image",
		Short: "images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewImageAddCommand())
	cmd.AddCommand(NewImageDeleteCommand())
	cmd.AddCommand(NewImageServeCommand())
	cmd.AddCommand(NewImageSaveCommand())

	/* f := cmd.Flags()
	   f.BoolV */

	return cmd
}
