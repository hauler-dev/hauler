package app

import "github.com/spf13/cobra"

type pkgBootOpts struct {
	cfgFile string
}

func NewPkgBootCommand() *cobra.Command {
	opts := pkgBootOpts{}

	cmd := &cobra.Command{
		Use:     "boot",
		Short:   "",
		Long:    "",
		Aliases: []string{"b", "bootstrap"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	return cmd
}

func (o *pkgBootOpts) PreRun() error {
	return nil
}

func (o *pkgBootOpts) Run() error {
	return nil
}
