package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/add"
)

func addAdd(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use: "add",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addAddImage(),
		addAddGeneric(),
		addAddPackage(),
	)

	parent.AddCommand(cmd)
}

func addAddGeneric() *cobra.Command {
	o := &add.GenericOpts{}

	cmd := &cobra.Command{
		Use:  "generic",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return add.GenericCmd(ctx, o, s, args...)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addAddImage() *cobra.Command {
	o := &add.ImageOpts{}

	cmd := &cobra.Command{
		Use:  "image",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return add.ImageCmd(ctx, o, s, args...)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addAddPackage() *cobra.Command {
	o := &add.PackageOpts{}

	cmd := &cobra.Command{
		Use:     "package",
		Aliases: []string{"p", "pkg"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return add.PackageCmd(ctx, o, s, args...)
		},
	}
	o.AddFlags(cmd)

	return cmd
}
