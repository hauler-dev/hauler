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

	// On Unix systems, the default is $TMPDIR if non-empty, else /tmp
	// On Windows, the default is GetTempPath, returning the first value from %TMP%, %TEMP%, %USERPROFILE%, or Windows directory
	f.StringSliceVarP(&o.FileName, "filename", "f", []string{consts.DefaultHaulerArchiveName}, "(Optional) Specify the name of inputted haul(s)")
}
