package app

import (
	"github.com/spf13/cobra"
)

type copyOpts struct {
	haul string
}

func NewCopyCommand() *cobra.Command {
	opts := &copyOpts{}

	cmd := &cobra.Command{
		Use:     "copy",
		Short:   "copy things",
		Long:    ``,
		Aliases: []string{"c", "cp"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args[0])
		},
	}

	return cmd
}

// Run performs the operation.
func (o *copyOpts) Run(haul string) error {
	//ctx, cancel := context.WithTimeout(context.Background(), timeout)
	//defer cancel()

	//dpl := deployer.NewDeployer()
	//if err := dpl.Deploy(ctx, haul); err != nil {
	//	return err
	//}

	return nil
}
