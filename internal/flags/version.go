package flags

import "github.com/spf13/cobra"

type VersionOpts struct {
	JSON bool
}

func (o *VersionOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.BoolVar(&o.JSON, "json", false, "Set the output format to JSON")
}
