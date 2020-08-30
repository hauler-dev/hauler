package app

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "k3ama",
		Short: "k3ama provides airgap migration assitance",
		Long: `k3ama provides airgap migration assistance using k3s.

Choose your functionality and create a package with internet access, then deploy the package into
your air-gapped environment.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewPackageCommand())

	return cmd
}
