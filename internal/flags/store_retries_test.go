package flags

import (
	"testing"

	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// TestAddFlagsRegistersRetries verifies the flag is registered as a
// persistent flag on the parent command, so every store subcommand inherits
// it rather than only `store sync`.
func TestAddFlagsRegistersRetries(t *testing.T) {
	o := &StoreRootOpts{}
	cmd := &cobra.Command{Use: "store"}
	o.AddFlags(cmd)

	f := cmd.PersistentFlags().Lookup("retries")
	if f == nil {
		t.Fatal("expected --retries to be registered as a persistent flag")
	}
	if f.DefValue != "0" {
		t.Fatalf("expected default value 0 (unset), got %q", f.DefValue)
	}
}

// TestStoreResolvesRetriesFromEnv verifies that a subcommand which never
// touches --retries directly -- `store add`, `store load`, `store copy` --
// still picks up HAULER_RETRIES.
func TestStoreResolvesRetriesFromEnv(t *testing.T) {
	t.Setenv(consts.HaulerRetries, "7")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Retries != 7 {
		t.Fatalf("got Retries %d, want 7", o.Retries)
	}
}

// TestStoreLeavesExplicitRetriesAlone verifies Store() does not overwrite an
// explicit --retries with HAULER_RETRIES.
func TestStoreLeavesExplicitRetriesAlone(t *testing.T) {
	t.Setenv(consts.HaulerRetries, "7")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{Retries: 2}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Retries != 2 {
		t.Fatalf("got Retries %d, want 2 (env must not override)", o.Retries)
	}
}

// TestStoreDefaultsRetriesWhenUnset verifies Store() falls back to
// consts.DefaultRetries when neither --retries nor HAULER_RETRIES is set.
func TestStoreDefaultsRetriesWhenUnset(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Retries != consts.DefaultRetries {
		t.Fatalf("got Retries %d, want default %d", o.Retries, consts.DefaultRetries)
	}
}

// TestStoreRejectsNegativeRetries verifies that a negative --retries
// surfaces an error instead of being silently dropped, same as
// --blob-concurrency.
func TestStoreRejectsNegativeRetries(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &StoreRootOpts{Retries: -5}
	if _, err := o.Store(t.Context(), &CliRootOpts{}); err == nil {
		t.Fatal("expected an error for negative --retries, got nil")
	}
}
