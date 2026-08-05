package flags

import "github.com/spf13/cobra"

type CreateManifestOpts struct {
	*StoreRootOpts

	Output string
}

func (o *CreateManifestOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Output, "output", "o", "", "(Optional) Path to write the generated manifest to (default \"$STORENAME-manifest.yaml\")")
}
