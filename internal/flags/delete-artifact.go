package flags

import "github.com/spf13/cobra"

type DeleteArtifactOpts struct {
	Force bool // skip delete confirmation
}

func (o *DeleteArtifactOpts) AddFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&o.Force, "force", "f", false, "(Optional) Delete artifacts without confirmation")
}
