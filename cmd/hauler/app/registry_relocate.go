package app

import (
	"github.com/spf13/cobra"
)

type registryRelocateOpts struct {
	*rootOpts
	*registryOpts
}

func NewRegistryRelocateCommand() *cobra.Command {
	opts := &registryRelocateOpts{
		rootOpts:     &ro,
		registryOpts: &reo,
	}

	cmd := &cobra.Command{
		Use:   "relocate",
		Short: "relocate haulers embedded registry contents to an existing registry",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	_ = f

	return cmd
}

func (o *registryRelocateOpts) PreRun() error {
	return nil
}

func (o *registryRelocateOpts) Run() error {
	return nil
}
