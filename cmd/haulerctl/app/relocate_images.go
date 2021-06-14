package app

import (
	"fmt"

	"github.com/spf13/cobra"
)

type relocateImagesOpts struct {
	relocate *relocateOpts
}

// NewRelocateImagesCommand creates a new sub command of relocate for images
func NewRelocateImagesCommand(relocate *relocateOpts) *cobra.Command {
	opts := &relocateImagesOpts{relocate: relocate}

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Use artifact from bundle images to populate a target registry with the artifact's images",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	return cmd
}

func (o *relocateImagesOpts) Run() error {
	//TODO
	fmt.Println("relocate images")
	fmt.Println(o.relocate.bundleDir)
	return nil
}
