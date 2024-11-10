package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"

	"hauler.dev/go/hauler/cmd/hauler/cli/store"
	"hauler.dev/go/hauler/internal/flags"
)

var rootStoreOpts = &flags.StoreRootOpts{}
var ro = &flags.CliRootOpts{}

func addStore(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:     "store",
		Aliases: []string{"s"},
		Short:   "Interact with the content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	rootStoreOpts.AddFlags(cmd)

	cmd.AddCommand(
		addStoreSync(),
		addStoreExtract(),
		addStoreLoad(),
		addStoreSave(),
		addStoreServe(),
		addStoreInfo(),
		addStoreCopy(),
		addStoreAdd(),
	)

	parent.AddCommand(cmd)
}

func addStoreExtract() *cobra.Command {
	o := &flags.ExtractOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:     "extract",
		Short:   "Extract artifacts from the content store to disk",
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
	o.AddFlags(cmd)

	return cmd
}

func addStoreSync() *cobra.Command {
	o := &flags.SyncOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync content to the content store",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}

			return store.SyncCmd(ctx, o, s, ro)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreLoad() *cobra.Command {
	o := &flags.LoadOpts{StoreRootOpts: rootStoreOpts}

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
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the content store via an OCI Compliant Registry or Fileserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		addStoreServeRegistry(),
		addStoreServeFiles(),
	)

	return cmd
}

// RegistryCmd serves the registry
func addStoreServeRegistry() *cobra.Command {
	o := &flags.ServeRegistryOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Serve the OCI Compliant Registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}

			return store.ServeRegistryCmd(ctx, o, s, ro)
		},
	}

	o.AddFlags(cmd)

	return cmd
}

// FileServerCmd serves the file server
func addStoreServeFiles() *cobra.Command {
	o := &flags.ServeFilesOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "fileserver",
		Short: "Serve the Fileserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}

			return store.ServeFilesCmd(ctx, o, s, ro)
		},
	}

	o.AddFlags(cmd)

	return cmd
}

func addStoreSave() *cobra.Command {
	o := &flags.SaveOpts{StoreRootOpts: rootStoreOpts}

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
	o.AddFlags(cmd)

	return cmd
}

func addStoreInfo() *cobra.Command {
	o := &flags.InfoOpts{StoreRootOpts: rootStoreOpts}

	var allowedValues = []string{"image", "chart", "file", "sigs", "atts", "sbom", "all"}

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
	o := &flags.CopyOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "copy",
		Short: "Copy all store content to another location",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}

			return store.CopyCmd(ctx, o, s, args[0], ro)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreAdd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add content to the store",
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
	o := &flags.AddFileOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "file",
		Short: "Add a file to the store",
		Example: `# fetch local file
hauler store add file file.txt

# fetch remote file
hauler store add file https://get.rke2.io/install.sh

# fetch remote file and assign new name
hauler store add file https://get.hauler.dev --name hauler-install.sh`,
		Args: cobra.ExactArgs(1),
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
	o := &flags.AddImageOpts{StoreRootOpts: rootStoreOpts}

	cmd := &cobra.Command{
		Use:   "image",
		Short: "Add a image to the store",
		Example: `# fetch image
hauler store add image busybox

# fetch image with repository and tag
hauler store add image library/busybox:stable

# fetch image with full image reference and specific platform
hauler store add image ghcr.io/hauler-dev/hauler-debug:v1.0.7 --platform linux/amd64

# fetch image with full image reference via digest
hauler store add image gcr.io/distroless/base@sha256:7fa7445dfbebae4f4b7ab0e6ef99276e96075ae42584af6286ba080750d6dfe5

# fetch image with full image reference, specific platform, and signature verification
hauler store add image rgcrprod.azurecr.us/hauler/rke2-manifest.yaml:v1.28.12-rke2r1 --platform linux/amd64 --key carbide-key.pub`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			s, err := o.Store(ctx)
			if err != nil {
				return err
			}

			return store.AddImageCmd(ctx, o, s, args[0], ro)
		},
	}
	o.AddFlags(cmd)

	return cmd
}

func addStoreAddChart() *cobra.Command {
	o := &flags.AddChartOpts{
		StoreRootOpts: rootStoreOpts,
		ChartOpts:     &action.ChartPathOptions{},
	}

	cmd := &cobra.Command{
		Use:   "chart",
		Short: "Add a helm chart to the store",
		Example: `# fetch local helm chart
hauler store add chart path/to/chart/directory --repo .

# fetch local compressed helm chart
hauler store add chart path/to/chart.tar.gz --repo .

# fetch remote oci helm chart
hauler store add chart hauler-helm --repo oci://ghcr.io/hauler-dev

# fetch remote oci helm chart with version
hauler store add chart hauler-helm --repo oci://ghcr.io/hauler-dev --version 1.0.6

# fetch remote helm chart
hauler store add chart rancher --repo https://releases.rancher.com/server-charts/stable

# fetch remote helm chart with specific version
hauler store add chart rancher --repo https://releases.rancher.com/server-charts/latest --version 2.9.1`,
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
