package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type SaveOpts struct {
	*StoreRootOpts
	FileName string
	Platform string
}

func (o *SaveOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.FileName, "filename", "f", consts.DefaultHaulerArchiveName, "(Optional) Specify the name of outputted archive")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform for runtime imports... i.e. linux/amd64 (unspecified implies all)")
}
