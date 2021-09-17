package app

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/store"
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
	f.StringVarP(&reo.root, "root", "r", "", "")
	f.StringVarP(&reo.port, "port", "p", ":3333", "Port to listen on")

	cmd.AddCommand(NewRegistryAddCommand())
	cmd.AddCommand(NewRegistryDeleteCommand())
	cmd.AddCommand(NewRegistryServeCommand())
	cmd.AddCommand(NewRegistrySaveCommand())

	return cmd
}

func (o *registryOpts) setup() error {
	return nil
}

func (o *registryOpts) buildRegistry(ctx context.Context) *store.Store {
	cfg := store.DefaultConfiguration(o.root, o.port)

	r, err := store.NewRegistry(ctx, cfg)
	if err != nil {
	} // TODO:

	return r
}
