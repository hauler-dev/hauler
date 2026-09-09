package flags

import (
	"testing"

	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// TestServeRegistryOptsAddFlagsRegistersBasicAuth verifies --basic-auth and --basic-auth-realm register with the right defaults.
func TestServeRegistryOptsAddFlagsRegistersBasicAuth(t *testing.T) {
	o := &ServeRegistryOpts{}
	cmd := &cobra.Command{Use: "registry"}
	o.AddFlags(cmd)

	auth := cmd.Flags().Lookup("basic-auth")
	if auth == nil {
		t.Fatal("expected --basic-auth to be registered")
	}
	if auth.DefValue != "" {
		t.Fatalf("expected --basic-auth default to be empty, got %q", auth.DefValue)
	}

	realm := cmd.Flags().Lookup("basic-auth-realm")
	if realm == nil {
		t.Fatal("expected --basic-auth-realm to be registered")
	}
	if realm.DefValue != consts.DefaultRegistryRealm {
		t.Fatalf("got --basic-auth-realm default %q, want %q", realm.DefValue, consts.DefaultRegistryRealm)
	}
}

// TestServeFilesOptsAddFlagsRegistersBasicAuth is the fileserver equivalent of the registry test above.
func TestServeFilesOptsAddFlagsRegistersBasicAuth(t *testing.T) {
	o := &ServeFilesOpts{}
	cmd := &cobra.Command{Use: "fileserver"}
	o.AddFlags(cmd)

	auth := cmd.Flags().Lookup("basic-auth")
	if auth == nil {
		t.Fatal("expected --basic-auth to be registered")
	}
	if auth.DefValue != "" {
		t.Fatalf("expected --basic-auth default to be empty, got %q", auth.DefValue)
	}

	realm := cmd.Flags().Lookup("basic-auth-realm")
	if realm == nil {
		t.Fatal("expected --basic-auth-realm to be registered")
	}
	if realm.DefValue != consts.DefaultFileserverRealm {
		t.Fatalf("got --basic-auth-realm default %q, want %q", realm.DefValue, consts.DefaultFileserverRealm)
	}
}
