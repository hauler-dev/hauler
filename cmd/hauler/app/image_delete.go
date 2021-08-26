package app

import (
	"github.com/spf13/cobra"
)

type imageDeleteOpts struct {
	port int
}

func NewImageDeleteCommand() *cobra.Command {
	opts := imageDeleteOpts{}

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "remove an image from the image store",
		Long: `
Given an image reference, remove it's layers and manifest(s) from the local image store.
Shared layers (layers used by other images) will be preserved.

    Example:

        # Tagless image
        hauler image delete rancher/rancher

        # Tagged images
        hauler image delete rancher/k3s:v1.21.0-k3s1 rancher/fleet:v1.21

        # Digest image
        hauler i d rancher/k3s@sha256:0795bbfb58ae334f49b8d71a8b2f1808d2a5220fd50d8553d7be3235235e7248
        `,
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"d", "del"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	_ = f

	return cmd
}

func (o imageDeleteOpts) Run() error {
	// TODO
	return nil
}
