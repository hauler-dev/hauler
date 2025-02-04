package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type LoadOpts struct {
	*StoreRootOpts
	FileName     []string
	TempOverride string
}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	// On Unix, the default is $TMPDIR if non-empty, else /tmp.
	// On Windows, the default is GetTempPath, returning the first non-empty value from %TMP%, %TEMP%, %USERPROFILE%, or the Windows directory.
	f.StringSliceVarP(&o.FileName, "filename", "f", []string{consts.DefaultHaulerArchiveName}, "(Optional) Specify the name of inputted archive(s)")
	f.StringVarP(&o.TempOverride, "tempdir", "t", "", "(Optional) Override the default temporary directory determined by the OS")
}
