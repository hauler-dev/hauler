package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/audit"
)

func TestAuditRegistryAuth(t *testing.T) {
	cases := map[string]struct {
		runErr     error
		level      string
		server     string
		wantLogged string
	}{
		"login success":        {level: "standard", server: "registry.example.local", wantLogged: `"command":"login","reference":"registry.example.local"`},
		"credentials stripped": {level: "standard", server: "https://user:token@registry.example.local/path?x=y", wantLogged: `"reference":"https://registry.example.local/path"`},
		"login failure":        {level: "standard", server: "registry.example.local", runErr: errors.New("unauthorized")},
		"audit level none":     {level: "none", server: "registry.example.local"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ro := &flags.CliRootOpts{AuditLevel: tc.level, HaulerDir: t.TempDir()}
			c := &cobra.Command{Use: "login", RunE: func(*cobra.Command, []string) error { return tc.runErr }}
			err := auditRegistryAuth(context.Background(), c, "login", ro).RunE(c, []string{tc.server})
			if !errors.Is(err, tc.runErr) {
				t.Fatalf("RunE error = %v, want %v", err, tc.runErr)
			}

			data, readErr := os.ReadFile(filepath.Join(ro.HaulerDir, audit.LogFileName))
			if tc.wantLogged == "" {
				if readErr == nil {
					t.Errorf("expected no audit entry, got %q", data)
				}
				return
			}
			if readErr != nil {
				t.Fatalf("reading audit.log: %v", readErr)
			}
			if !strings.Contains(string(data), tc.wantLogged) || strings.Contains(string(data), "token") {
				t.Errorf("audit.log = %q, want it to contain %q and no credentials", data, tc.wantLogged)
			}
		})
	}
}
