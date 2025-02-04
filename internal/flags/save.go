package flags

import (
	"github.com/spf13/cobra"
)

type SaveOpts struct {
	*StoreRootOpts
	Platform string
}

func (o *SaveOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform for runtime imports... i.e. linux/amd64 (unspecified implies all)")
}
