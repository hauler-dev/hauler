package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/store"
)

func addStore(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use: "store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addStoreBuild(),
		addStoreLock(),
	)

	parent.AddCommand(cmd)
}

func addStoreBuild() *cobra.Command {
	o := &store.BuildOpts{}

	cmd := &cobra.Command{
		Use: "build",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.BuildCmd(ctx, o, s)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreLock() *cobra.Command {
	o := &store.LockOpts{}

	cmd := &cobra.Command{
		Use: "lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			return store.LockCmd(ctx, o)
		},
	}
	o.AddFlags(cmd)

	return cmd
}
