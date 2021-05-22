package app

import (
	"github.com/spf13/cobra"
)

type bundleOpts struct {
	bundleDir string
}

func NewBundleCommand() *cobra.Command {
	opts := &bundleOpts{}

	cmd := &cobra.Command{
		Use:     "bundle",
		Short:   "bundle images or artifact for relocation",
		Long:    "",
		Aliases: []string{"b"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	f := cmd.PersistentFlags()
	f.StringVarP(&opts.bundleDir, "bundledir", "b", "./bundle",
		"directory locating a bundle, if one exists we will append (./bundle)")

	cmd.AddCommand(NewBundleArtifactsCommand())
	cmd.AddCommand(NewBundleImagesCommand())

	return cmd
}
