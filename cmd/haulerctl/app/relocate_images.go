package app

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type relocateImagesOpts struct {
	relocateOpts
}

// NewRelocateImagesCommand creates a new sub command of relocate for images
func NewRelocateImagesCommand() *cobra.Command {
	opts := &relocateImagesOpts{}

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Use artifact from bundle images to populate a target registry with the artifact's images",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.relocateOpts.bundleDir = viper.GetString("bundledir")
			return opts.Run()
		},
	}

	return cmd
}

func (o *relocateImagesOpts) Run() error {
	//TODO
	fmt.Println("relocate images")
	fmt.Println(o.relocateOpts.bundleDir)
	return nil
}
