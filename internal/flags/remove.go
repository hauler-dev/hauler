package flags

import "github.com/spf13/cobra"

type RemoveOpts struct {
	Force bool // skip remove confirmation
}

func (o *RemoveOpts) AddFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVarP(&o.Force, "force", "f", false, "(Optional) Remove artifact(s) without confirmation")
}
