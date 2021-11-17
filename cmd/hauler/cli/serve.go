package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/serve"
)

func addServe(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run one or more of hauler's embedded servers types",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addServeFiles(),
		addServeRegistry(),
	)

	parent.AddCommand(cmd)
}

func addServeFiles() *cobra.Command {
	o := &serve.FilesOpts{}
	cmd := &cobra.Command{
		Use:   "files",
		Short: "Start a fileserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return serve.FilesCmd(ctx, o)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addServeRegistry() *cobra.Command {
	o := &serve.RegistryOpts{}

	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Start a registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			return serve.RegistryCmd(ctx, o)
		},
	}
	o.AddFlags(cmd)

	return cmd
}
