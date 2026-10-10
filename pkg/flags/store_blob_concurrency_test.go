package flags

import (
	"testing"

	"github.com/spf13/cobra"
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
