package app

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hauler",
		Short: "hauler provides airgap migration assitance",
		Long: `hauler provides airgap migration assistance using k3s.

Choose your functionality and create a package when internet access is available,
then deploy the package into your air-gapped environment.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewPackageCommand())

	return cmd
}
