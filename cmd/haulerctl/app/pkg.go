package app

import "github.com/spf13/cobra"

type pkgOpts struct{}

func NewPkgCommand() *cobra.Command {
	opts := &pkgOpts{}
	//TODO
	_ = opts

	cmd := &cobra.Command{
		Use:     "pkg",
		Short:   "",
		Long:    "",
		Aliases: []string{"p", "package"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewPkgCreateCommand())
	cmd.AddCommand(NewPkgBootCommand())

	return cmd
}
