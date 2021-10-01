package app

import (
	"github.com/spf13/cobra"
)

type driverOpts struct {
	*rootOpts
}

func NewDriverCommand() *cobra.Command {
	o := driverOpts{
		&ro,
	}
	_ = o

	cmd := &cobra.Command{
		Use:     "driver",
		Short:   "driver stuff",
		Aliases: []string{"d"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewDriverPackageCommand())
	cmd.AddCommand(NewDriverConfigCommand())

	f := cmd.Flags()
	_ = f

	return cmd
}
