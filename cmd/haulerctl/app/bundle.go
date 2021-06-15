package app

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type bundleOpts struct {
	bundleDir string
}

// NewBundleCommand creates a new sub command under
// haulterctl for bundling images and artifacts
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

	cmd.AddCommand(NewBundleArtifactsCommand(opts))

	viper.AutomaticEnv()

	return cmd
}
