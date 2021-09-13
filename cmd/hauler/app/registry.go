package app

import "github.com/spf13/cobra"

func NewRegistryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewRegistryAddCommand())
	cmd.AddCommand(NewRegistryDeleteCommand())
	cmd.AddCommand(NewRegistryServeCommand())
	cmd.AddCommand(NewRegistrySaveCommand())

	/* f := cmd.Flags()
	   f.BoolV */

	return cmd
}
