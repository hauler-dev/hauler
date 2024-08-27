package flags

import "github.com/spf13/cobra"

type SaveOpts struct {
	*StoreRootOpts
	FileName string
}

func (o *SaveOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.FileName, "filename", "f", "haul.tar.zst", "(Optional) Specify the name of outputted archive")
}
