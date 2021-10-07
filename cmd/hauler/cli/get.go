package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/get"
)

func addGet(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use: "get",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addGetImage(),
		addGetGeneric(),
	)

	parent.AddCommand(cmd)
}

func addGetImage() *cobra.Command {
	o := &get.ImageOpts{}

	cmd := &cobra.Command{
		Use: "image",
		RunE: func(cmd *cobra.Command, args []string) error {
			return get.ImageCmd(cmd.Context(), o)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addGetGeneric() *cobra.Command {
	o := &get.GenericOpts{}

	cmd := &cobra.Command{
		Use:  "generic",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return get.GenericCmd(cmd.Context(), o, args...)
		},
	}
	o.AddFlags(cmd)

	return cmd
}
