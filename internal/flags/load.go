package flags

import "github.com/spf13/cobra"

type LoadOpts struct {
	*StoreRootOpts
	TempOverride string
}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	// On Unix systems, the default is $TMPDIR if non-empty, else /tmp.
	// On Windows, the default is GetTempPath, returning the first non-empty
	// value from %TMP%, %TEMP%, %USERPROFILE%, or the Windows directory.
	// On Plan 9, the default is /tmp.
	f.StringVarP(&o.TempOverride, "tempdir", "t", "", "(Optional) Override the default temporary directiory determined by the OS")
}
