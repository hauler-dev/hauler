package store

import (
	"os"
	"slices"

	"github.com/opencontainers/go-digest"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/audit"
)

// auditLevel returns the resolved audit level (none, standard, verbose)
func auditLevel(ro *flags.CliRootOpts) string {
	return audit.Level(ro)
}

// withoutAudit returns a copy of ro with auditing off, for a command run internally by another (i.e. serve calling copy)
func withoutAudit(ro *flags.CliRootOpts) *flags.CliRootOpts {
	if ro == nil {
		return nil
	}
	quiet := *ro
	quiet.AuditLevel = "none"
	return &quiet
}

// sortedDigests returns digests sorted with duplicates removed
func sortedDigests(digests []string) []string {
	out := slices.Clone(digests)
	slices.Sort(out)
	return slices.Compact(out)
}

// fileDigest returns the sha256 digest of a local file, or "" if it cannot be read
func fileDigest(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	d, err := digest.FromReader(f)
	if err != nil {
		return ""
	}
	return d.String()
}
