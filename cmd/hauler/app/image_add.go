package app

import (
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/rancherfederal/hauler/pkg/store"
	"github.com/spf13/cobra"
)

type imageAddOpts struct {
	path string
}

func NewImageAddCommand() *cobra.Command {
	opts := imageAddOpts{}

	cmd := &cobra.Command{
		Use:   "add",
		Short: "add an image to the image store",
		Long: `
Given an image reference, add it's layers and manifest(s) to the local image store.

    Example:

        # Tagless image
        hauler image add rancher/rancher

        # Tagged images
        hauler image add rancher/k3s:v1.21.0-k3s1

        # Digest image
        hauler i a rancher/k3s@sha256:0795bbfb58ae334f49b8d71a8b2f1808d2a5220fd50d8553d7be3235235e7248
        `,
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"a"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run(args)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.path, "path", "p", "hauler", "Relative path to OCI store.")

	return cmd
}

func (o imageAddOpts) Run(args []string) error {
	// Ensure we have a store
	s, err := store.NewOci(o.path)
	if err != nil {
		return err
	}

	for _, arg := range args {
		ref, err := name.ParseReference(arg)
		if err != nil {
			return err
		}

		if err := s.Add(ref); err != nil {
			return err
		}
	}

	return nil
}
