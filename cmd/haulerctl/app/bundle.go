package app

import (
	"context"

	"github.com/rancherfederal/hauler/pkg/bundle"
	"github.com/spf13/cobra"
)

type bundleOpts struct {
	bundleDir string
}

func NewBundleCommand() *cobra.Command {
	opts := &bundleOpts{}

	cmd := &cobra.Command{
		Use:     "bundle",
		Short:   "bundle images for relocation",
		Long:    "",
		Aliases: []string{"b"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.bundleDir, "bundledir", "b", "./bundle",
		"directory locating a bundle, if one exists we will append (./bundle)")

	return cmd
}

func (o *bundleOpts) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	b := bundle.NewLayoutStore(o.bundleDir)

	images := []string{"alpine:latest", "registry:2.7.1"}

	for _, i := range images {
		if err := b.Add(ctx, i); err != nil {
			return err
		}
	}

	return nil
}
