package app

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1beta1"
	"github.com/spf13/cobra"
)

type bundleBootOpts struct {
	bundleOpts bundleOpts

	name string
	new  bool
}

func NewBundleBootCommand() *cobra.Command {
	opts := &bundleBootOpts{}

	cmd := &cobra.Command{
		Use: "boot",
		Short: "does something",
		RunE: func(cmd *cobra.Command, args []string) error {

			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.name, "name", "n", "hauler",
		"Name of the bundle to new")
	f.BoolVar(&opts.new, "new", false,
		"Toggle creation of an empty bundle in the current directory")

	return cmd
}

func (o *bundleBootOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var b *v1beta1.BootBundle
	if o.new {
		b = v1beta1.NewBootBundle(o.name)
		err := b.Save()
		if err != nil {
			return err
		}
	}

	_ = ctx
	return nil
}
