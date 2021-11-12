package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/store"
)

func addStore(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "store",
		Aliases: []string{"s"},
		Short:   "Interact with hauler's embedded content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addStoreSync(),
		addStoreExtract(),
		addStoreLoad(),
		addStoreSave(),
		addStoreServe(),
		addStoreList(),
		addStoreCopy(),

		// TODO: Remove this in favor of sync?
		addStoreAdd(),
	)

	parent.AddCommand(cmd)
}

func addStoreExtract() *cobra.Command {
	o := &store.ExtractOpts{}

	cmd := &cobra.Command{
		Use:     "extract",
		Short:   "Extract content from the store to disk",
		Aliases: []string{"x"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.ExtractCmd(ctx, o, s, args[0])
		},
	}
	o.AddArgs(cmd)

	return cmd
}

func addStoreSync() *cobra.Command {
	o := &store.SyncOpts{}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync content to the embedded content store",
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

func addStoreLoad() *cobra.Command {
	o := &store.LoadOpts{}

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Load a content store from a store archive",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.LoadCmd(ctx, o, s.DataDir, args...)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreServe() *cobra.Command {
	o := &store.ServeOpts{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Expose the content of a local store through an OCI compliant server",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.ServeCmd(ctx, o, s)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreSave() *cobra.Command {
	o := &store.SaveOpts{}

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a content store to a store archive",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.SaveCmd(ctx, o, o.FileName, s.DataDir)
		},
	}
	o.AddArgs(cmd)

	return cmd
}

func addStoreList() *cobra.Command {
	o := &store.ListOpts{}

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all content references in a store",
		Args:    cobra.ExactArgs(0),
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.ListCmd(ctx, o, s)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreCopy() *cobra.Command {
	o := &store.CopyOpts{}

	cmd := &cobra.Command{
		Use:   "copy",
		Short: "Copy all store contents to another OCI registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.CopyCmd(ctx, o, s, args[0])
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreAdd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add content to store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addStoreAddFile(),
		addStoreAddImage(),
		addStoreAddChart(),
	)

	return cmd
}

func addStoreAddFile() *cobra.Command {
	o := &store.AddFileOpts{}

	cmd := &cobra.Command{
		Use:   "file",
		Short: "Add a file to the content store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.AddFileCmd(ctx, o, s, args[0])
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreAddImage() *cobra.Command {
	o := &store.AddImageOpts{}

	cmd := &cobra.Command{
		Use:   "image",
		Short: "Add an image to the content store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.AddImageCmd(ctx, o, s, args[0])
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreAddChart() *cobra.Command {
	o := &store.AddChartOpts{}

	cmd := &cobra.Command{
		Use:   "chart",
		Short: "Add a chart to the content store",
		Example: `
# add a chart
hauler store add chart longhorn --repo "https://charts.longhorn.io"

# add a specific version of a chart
hauler store add chart rancher --repo "https://releases.rancher.com/server-charts/latest" --version "2.6.2"
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := ro.getStore(ctx)
			if err != nil {
				return err
			}

			return store.AddChartCmd(ctx, o, s, args[0])
		},
	}
	o.AddFlags(cmd)

	return cmd
}
