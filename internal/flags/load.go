package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type LoadOpts struct {
	*StoreRootOpts
	FileName     string
	TempOverride string
}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	// On Unix, the default is $TMPDIR if non-empty, else /tmp.
	// On Windows, the default is GetTempPath, returning the first non-empty value from %TMP%, %TEMP%, %USERPROFILE%, or the Windows directory.
	f.StringVarP(&o.FileName, "filename", "f", consts.DefaultHaulArchiveName, "(Optional) Specify the name of inputted archive")
	f.StringVarP(&o.TempOverride, "tempdir", "t", "", "(Optional) Override the default temporary directiory determined by the OS")
}
