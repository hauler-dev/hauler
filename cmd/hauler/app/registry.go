package app

import (
	"os"

	"github.com/spf13/cobra"
)

// TODO: Replace with viper or config parser
type registryOpts struct {
	*rootOpts

	root string
	port string
}

var reo registryOpts

func NewRegistryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "registry",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return reo.setup()
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	f := cmd.PersistentFlags()
	_ = f

	cmd.AddCommand(NewRegistryAddCommand())
	cmd.AddCommand(NewRegistryDeleteCommand())
	cmd.AddCommand(NewRegistryServeCommand())
	cmd.AddCommand(NewRegistrySaveCommand())
	cmd.AddCommand(NewRegistryRelocateCommand())

	return cmd
}

func (o *registryOpts) setup() error {
	return nil
}
