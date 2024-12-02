package flags

import "github.com/spf13/cobra"

type CliRootOpts struct {
	LogLevel     string
	HaulerDir    string
	IgnoreErrors bool
}

func AddRootFlags(cmd *cobra.Command, ro *CliRootOpts) {
	pf := cmd.PersistentFlags()

	pf.StringVarP(&ro.LogLevel, "log-level", "l", "info", "Set the logging level (i.e. info, debug, warn)")
	pf.StringVarP(&ro.HaulerDir, "haulerdir", "d", "", "Set the location of the hauler directory (default $HOME/.hauler)")
	pf.BoolVar(&ro.IgnoreErrors, "ignore-errors", false, "Ignore/Bypass errors (i.e. warn on error) (defaults false)")
}
