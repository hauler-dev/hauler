package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/store"
)

func addStore(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Interact with hauler's content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addStoreSync(),
		addStoreGet(),
	)

	parent.AddCommand(cmd)
}

func addStoreGet() *cobra.Command {
	o := &store.GetOpts{}

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get content from hauler's embedded content store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.GetCmd(ctx, o, s, args[0])
		},
	}
	o.AddArgs(cmd)

	return cmd
}

func addStoreSync() *cobra.Command {
	o := &store.SyncOpts{}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync content to hauler's embedded content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.SyncCmd(ctx, o, s)
		},
	}
	o.AddFlags(cmd)

	return cmd
}
