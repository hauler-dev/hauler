package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type LoadOpts struct {
	*StoreRootOpts
	FileName []string
}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.FileName, "filename", "f", []string{consts.DefaultHaulerArchiveName}, "(Optional) Specify the name of inputted haul(s)")
}
