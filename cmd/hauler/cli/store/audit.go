package store

import "hauler.dev/go/hauler/v2/internal/flags"

// auditLevel returns the resolved audit level (none, standard, verbose)
func auditLevel(ro *flags.CliRootOpts) string {
	return flags.AuditLevel(ro)
}
