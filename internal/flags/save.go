package flags

import "github.com/spf13/cobra"

type SaveOpts struct {
	*StoreRootOpts
	FileName string
	Platform string
}

func (o *SaveOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.FileName, "filename", "f", "haul.tar.zst", "(Optional) Specify the name of outputted archive")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform for runtime imports... i.e. linux/amd64 (unspecified implies all)")
}
