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
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specifiy the platform of the images for the outputted archive... i.e. linux/amd64 (defaults to all)")
}
