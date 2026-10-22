package flags

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

type StoreRootOpts struct {
	StoreDir     string
	Retries      int
	TempOverride string

	// BlobConcurrency overrides the store's default blob-write concurrency
	// ceiling (content.OCI.blobSem) when > 0, bound to the
	// --blob-concurrency persistent flag (0 means "auto"). Populated by one
	// of two idempotent paths: `store sync`'s PreRunE (derives a value from
	// --concurrency when none was given) or store.Open() itself (consults
	// HAULER_BLOB_CONCURRENCY, so subcommands with no PreRunE still honor
	// the env var).
	BlobConcurrency int
}

func (o *StoreRootOpts) AddFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVarP(&o.StoreDir, "store", "s", "", "Set the directory to use for the content store")
	pf.IntVarP(&o.Retries, "retries", "r", 0, fmt.Sprintf("Set the number of retries for operations (0 uses HAULER_RETRIES, otherwise defaults to %d)", consts.DefaultRetries))
	pf.StringVarP(&o.TempOverride, "tempdir", "t", "", "(Optional) Override the default temporary directory determined by the OS")
	pf.IntVar(&o.BlobConcurrency, "blob-concurrency", 0, fmt.Sprintf("(Optional) Override the maximum number of concurrent blob writes (0 auto-derives from --concurrency where set, otherwise defaults to %d)", consts.DefaultBlobConcurrency))
}

// ResolveWorkDir returns the configured output dir, or "" to mean the current directory (legacy behavior).
func ResolveWorkDir(ro *CliRootOpts) string {
	if ro != nil && ro.WorkDir != "" {
		return ro.WorkDir
	}
	return os.Getenv(consts.HaulerWorkDir)
}
