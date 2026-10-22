package flags

import (
	"testing"

	"github.com/spf13/cobra"
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
