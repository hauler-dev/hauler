package app

import "github.com/spf13/cobra"

type bundleOpts struct {}

func NewBundleCommand() *cobra.Command {
	opts := bundleOpts{}
	_ = opts

	cmd := &cobra.Command{
		Use: "bundle",
		Short: "bundle stuff",
		Aliases: []string{"b", "bun"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewBundleCreateCommand())

	f := cmd.Flags()
	_ = f

	return cmd
}
