package flags

import "github.com/spf13/cobra"

type ExtractOpts struct {
	*StoreRootOpts
	DestinationDir string
}

func (o *ExtractOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.DestinationDir, "output", "o", "", "(Optional) Set the directory to output (defaults to current directory)")
}
