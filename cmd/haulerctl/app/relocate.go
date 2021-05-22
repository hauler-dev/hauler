package app

import (
	"github.com/spf13/cobra"
)

type relocateOpts struct {
	bundleDir string
}

func NewRelocateCommand() *cobra.Command {
	opts := &relocateOpts{}

	cmd := &cobra.Command{
		Use:     "relocate",
		Short:   "relocate images or artifacts to a registry",
		Long:    "",
		Aliases: []string{"r"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	f := cmd.PersistentFlags()
	f.StringVarP(&opts.bundleDir, "bundledir", "b", "./bundle",
		"directory locating a bundle, if one exists we will append (./bundle)")

	cmd.AddCommand(NewRelocateArtifactsCommand())
	cmd.AddCommand(NewRelocateImagesCommand())

	return cmd
}
