package flags

import "github.com/spf13/cobra"

type ExtractOpts struct {
	*StoreRootOpts
	DestinationDir string
}

func (o *ExtractOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.DestinationDir, "output", "o", "", "Directory to save contents to (defaults to current directory)")
}
