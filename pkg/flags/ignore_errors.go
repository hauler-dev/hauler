package flags

import (
	"os"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// ShouldIgnoreErrors reports whether the CLI should treat operation failures as
// non-fatal (log + continue) rather than aborting, based on the --ignore-errors
// flag or the HAULER_IGNORE_ERRORS environment variable. It is a pure read: it
// never mutates ro. This replaces the old pattern of writing os.Getenv's result
// back into ro.IgnoreErrors, which was a data race the moment callers ran
// concurrently (ro is a single *CliRootOpts shared across the whole CLI
// invocation).
func ShouldIgnoreErrors(ro *CliRootOpts) bool {
	if ro == nil {
		return false
	}
	if ro.IgnoreErrors {
		return true
	}
	return os.Getenv(consts.HaulerIgnoreErrors) == "true"
}
