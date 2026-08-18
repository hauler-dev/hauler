package flags

import "github.com/spf13/cobra"

type CliRootOpts struct {
	LogLevel     string
	HaulerDir    string
	IgnoreErrors bool
<<<<<<< HEAD
=======
	AuditLevel   string
	WorkDir      string
>>>>>>> 29a5c69 (fixed bug with store sync extraction into unwritable directory (#737))
}

func AddRootFlags(cmd *cobra.Command, ro *CliRootOpts) {
	pf := cmd.PersistentFlags()

	pf.StringVarP(&ro.LogLevel, "log-level", "l", "info", "Set the logging level (i.e. info, debug, warn)")
	pf.StringVarP(&ro.HaulerDir, "haulerdir", "d", "", "Set the location of the hauler directory (default $HOME/.hauler)")
<<<<<<< HEAD
	pf.BoolVar(&ro.IgnoreErrors, "ignore-errors", false, "Ignore/Bypass errors (i.e. warn on error) (defaults false)")
=======
	pf.BoolVar(&ro.IgnoreErrors, "ignore-errors", false, "Warn and continue instead of failing on errors, including storing images that failed verification (defaults false)")
	pf.StringVar(&ro.AuditLevel, "audit-level", "", "Set the audit logging level (none, standard, verbose) (defaults standard)")
	pf.StringVarP(&ro.WorkDir, "work-dir", "w", "", "(Optional) Set the directory for output that commands would otherwise write to the current directory (default: current directory)")
>>>>>>> 29a5c69 (fixed bug with store sync extraction into unwritable directory (#737))
}
