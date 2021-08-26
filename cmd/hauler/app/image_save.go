package app

import (
	"github.com/spf13/cobra"
)

type imageSaveOpts struct {
	name string
}

func NewImageSaveCommand() *cobra.Command {
	opts := imageSaveOpts{}

	cmd := &cobra.Command{
		Use:   "save",
		Short: "save the image store as a compressed archive",
		Long: `
Archive and compress a local image store.

    Example:

        # Archive and compress as a .tar.zstd
        hauler image save $PATH_TO_STORE

        # Name the resulting file
        hauler image save $PATH_TO_STORE --name steve.tzst
        `,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.name, "name", "n", "hauler", "Name of the archive to save.")

	return cmd
}

func (o imageSaveOpts) Run(args []string) error {
	// TODO
	return nil
}
