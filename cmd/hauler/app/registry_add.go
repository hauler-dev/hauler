package app

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/content"
)

type registryAddOpts struct {
	*rootOpts
	*registryOpts
}

func NewRegistryAddCommand() *cobra.Command {
	opts := &registryAddOpts{
		registryOpts: &reo,
	}

	cmd := &cobra.Command{
		Use:   "add",
		Short: "add an image to the registry store",
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

	// f := cmd.Flags()

	return cmd
}

func (o *registryAddOpts) Run(args []string) error {
	l := ro.Logger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := o.getStore(ctx, "")
	if err != nil {
		return err
	}

	s.Start()
	defer s.Stop()

	var imgs []content.Oci
	for _, ref := range args {
		imgs = append(imgs, content.NewImage(ref))
	}

	l.Infof("adding images")
	if err := s.Add(ctx, imgs...); err != nil {
		return err
	}

	var generics []content.Oci
	g, err := content.NewGeneric("", "hauler:v1", "something.hauler", "dist")
	if err != nil {
		return err
	}

	generics = append(generics, g)

	l.Infof("adding generics")
	if err := s.Add(ctx, generics...); err != nil {
		return err
	}

	return nil
}
