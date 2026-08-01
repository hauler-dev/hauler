package flags

import (
	"testing"

	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// TestAddFlagsRegistersBlobConcurrency verifies the flag is registered as a
// persistent flag on the parent command, so every store subcommand inherits
// it rather than only `store sync`.
func TestAddFlagsRegistersBlobConcurrency(t *testing.T) {
	o := &StoreRootOpts{}
	cmd := &cobra.Command{Use: "store"}
	o.AddFlags(cmd)

	f := cmd.PersistentFlags().Lookup("blob-concurrency")
	if f == nil {
		t.Fatal("expected --blob-concurrency to be registered as a persistent flag")
	}
	if f.DefValue != "0" {
		t.Fatalf("expected default value 0 (auto), got %q", f.DefValue)
	}
}

// TestStoreResolvesBlobConcurrencyFromEnv verifies that a subcommand which
// never runs sync's PreRunE -- `store add`, `store load`, `store copy` --
// still picks up HAULER_BLOB_CONCURRENCY.
func TestStoreResolvesBlobConcurrencyFromEnv(t *testing.T) {
	t.Setenv(consts.HaulerBlobConcurrency, "3")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.BlobConcurrency != 3 {
		t.Fatalf("got BlobConcurrency %d, want 3", o.BlobConcurrency)
	}
}

// TestStoreLeavesExplicitBlobConcurrencyAlone verifies Store() does not
// overwrite a value already resolved by sync's PreRunE.
func TestStoreLeavesExplicitBlobConcurrencyAlone(t *testing.T) {
	t.Setenv(consts.HaulerBlobConcurrency, "3")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{BlobConcurrency: 20}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.BlobConcurrency != 20 {
		t.Fatalf("got BlobConcurrency %d, want 20 (env must not override)", o.BlobConcurrency)
	}
}

// TestStoreRejectsNegativeBlobConcurrency verifies that a negative
// --blob-concurrency passed to a subcommand with no PreRunE of its own (add,
// load, copy, serve, extract) surfaces an error instead of being silently
// dropped. Before the fix, Store() only called ResolveBlobConcurrency when
// o.BlobConcurrency == 0, so a negative value skipped validation entirely
// and was never rejected.
func TestStoreRejectsNegativeBlobConcurrency(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{BlobConcurrency: -5}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err == nil {
		t.Fatal("expected an error for negative --blob-concurrency, got nil")
	}
}
