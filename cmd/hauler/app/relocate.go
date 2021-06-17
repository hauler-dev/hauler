package app

import (
	"github.com/spf13/cobra"
)

type relocateOpts struct {
	inputFile string
}

var rlo relocateOpts

// NewRelocateCommand creates a new sub command under
// haulterctl for relocating images and artifacts
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
	f.StringVarP(&opts.inputFile, "input", "i", "haul.tar.zst",
		"package output location relative to the current directory (haul.tar.zst)")

	cmd.AddCommand(NewRelocateArtifactsCommand())
	cmd.AddCommand(NewRelocateImagesCommand())

	return cmd
}
