package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type relocateArtifactsOpts struct {
	relocateOpts
}

// NewRelocateArtifactsCommand creates a new sub command of relocate for artifacts
func NewRelocateArtifactsCommand() *cobra.Command {
	opts := &relocateArtifactsOpts{}

	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Use artifact from bundle artifacts to populate a target file server with the artifact's contents",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.relocateOpts.bundleDir = viper.GetString("bundledir")
			return opts.Run()
		},
	}

	return cmd
}

func (o *relocateArtifactsOpts) Run() error {
	fmt.Println("relocate artifacts")
	fmt.Println(o.relocateOpts.bundleDir)
	return nil
}
