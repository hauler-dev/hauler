package cli

import (
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"fmt"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/store"
)

var rootStoreOpts = &store.RootOpts{}

func addStore(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "store",
		Aliases: []string{"s"},
		Short:   "Interact with hauler's embedded content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	rootStoreOpts.AddArgs(cmd)

	cmd.AddCommand(
		addStoreSync(),
		addStoreExtract(),
		addStoreLoad(),
		addStoreSave(),
		addStoreServe(),
		addStoreInfo(),
		addStoreCopy(),

		// TODO: Remove this in favor of sync?
		addStoreAdd(),
	)

	parent.AddCommand(cmd)
}

func addStoreExtract() *cobra.Command {
	o := &store.ExtractOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:     "extract",
		Short:   "Extract content from the store to disk",
		Aliases: []string{"x"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
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
	o := &store.SyncOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync content to the embedded content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
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
	o := &store.LoadOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Load a content store from a store archive",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}
			_ = s

			return store.LoadCmd(ctx, o, args...)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreServe() *cobra.Command {
	o := &store.ServeOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Expose the content of a local store through an OCI compliant registry or file server",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
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
	o := &store.SaveOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a content store to a store archive",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}
			_ = s

			return store.SaveCmd(ctx, o, o.FileName)
		},
	}
	o.AddArgs(cmd)

	return cmd
}

func addStoreInfo() *cobra.Command {
	o := &store.InfoOpts{RootOpts: rootStoreOpts}

	var allowedValues = []string{"image", "chart", "file", "all"}

	cmd := &cobra.Command{
		Use:     "info",
		Short:   "Print out information about the store",
		Args:    cobra.ExactArgs(0),
		Aliases: []string{"i", "list", "ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}
			
			for _, allowed := range allowedValues {
				if o.TypeFilter == allowed {
					return store.InfoCmd(ctx, o, s)
				}
			}
			return fmt.Errorf("type must be one of %v", allowedValues)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreCopy() *cobra.Command {
	o := &store.CopyOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "copy",
		Short: "Copy all store contents to another OCI registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
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
	o := &store.AddFileOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "file",
		Short: "Add a file to the content store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
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
	o := &store.AddImageOpts{RootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "image",
		Short: "Add an image to the content store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
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
	o := &store.AddChartOpts{
		RootOpts:  rootStoreOpts,
		ChartOpts: &action.ChartPathOptions{},
	}

	cmd := &cobra.Command{
		Use:   "chart",
		Short: "Add a local or remote chart to the content store",
		Example: `
# add a local chart
hauler store add chart path/to/chart/directory

# add a local compressed chart
hauler store add chart path/to/chart.tar.gz

# add a remote chart
hauler store add chart longhorn --repo "https://charts.longhorn.io"

# add a specific version of a chart
hauler store add chart rancher --repo "https://releases.rancher.com/server-charts/latest" --version "2.6.2"
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}

			return store.AddChartCmd(ctx, o, s, args[0])
		},
	}
	o.AddFlags(cmd)

	return cmd
}
