package flags

// AuditLevel returns the resolved audit level (none, standard, verbose).
func AuditLevel(ro *CliRootOpts) string {
	if ro == nil {
		return "none"
	}
	if ro.AuditLevel == "" {
		return "standard"
	}
	return ro.AuditLevel
}
