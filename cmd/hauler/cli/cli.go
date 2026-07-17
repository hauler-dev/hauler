package cli

import (
	"context"
	"fmt"
	"os"
	"os/user"

	cranecmd "github.com/google/go-containerregistry/cmd/crane/cmd"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
)

func New(ctx context.Context, ro *flags.CliRootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hauler",
		Short:   "Airgap Swiss Army Knife",
		Example: "  View the Docs: https://docs.hauler.dev\n  Environment Variables: " + consts.HaulerDir + " | " + consts.HaulerTempDir + " | " + consts.HaulerStoreDir + " | " + consts.HaulerIgnoreErrors + " | " + consts.HaulerLogLevel + " | " + consts.HaulerAuditLevel + "\n  Warnings: Hauler commands and flags marked with (EXPERIMENTAL) are not yet stable and may change in the future.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// check for log level env variable or flag
			if ro.LogLevel == "" {
				ro.LogLevel = os.Getenv(consts.HaulerLogLevel)
			}
			// default to info log level
			if ro.LogLevel == "" {
				ro.LogLevel = "info"
			}

			// check for audit level env variable or flag
			if ro.AuditLevel == "" {
				ro.AuditLevel = os.Getenv(consts.HaulerAuditLevel)
			}
			// default to standard audit level
			if ro.AuditLevel == "" {
				ro.AuditLevel = "standard"
			}
			switch ro.AuditLevel {
			case "none", "standard", "verbose":
			default:
				return fmt.Errorf("invalid --audit-level %q: must be one of none, standard, verbose", ro.AuditLevel)
			}

			l := log.FromContext(ctx)
			l.SetLevel(ro.LogLevel)
			l.Debugf("running cli command [%s]", cmd.CommandPath())

			// without a $HOME set... credential lookup in ~/.docker/config.json breaks
			// fall back to the current user's real home dir instead of the plain $HOME env var
			if os.Getenv("HOME") == "" {
				if u, err := user.Current(); err == nil && u.HomeDir != "" {
					l.Debugf("$HOME unset... using [%s] resolved from the current user", u.HomeDir)
					if err := os.Setenv("HOME", u.HomeDir); err != nil {
						l.Warnf("failed to set $HOME to [%s]: %v", u.HomeDir, err)
					}
				} else if home, herr := os.UserHomeDir(); herr == nil && home != "" {
					l.Debugf("$HOME unset... using [%s] resolved from the operating system", home)
					if err := os.Setenv("HOME", home); err != nil {
						l.Warnf("failed to set $HOME to [%s]: %v", home, err)
					}
				} else {
					l.Warnf("$HOME unset and could not be resolved... %v / %v", err, herr)
				}
			}

			// suppress WARN level messages from containerd
			// and other libraries that use the global logrus logger
			if ro.LogLevel == "debug" {
				logrus.SetLevel(logrus.DebugLevel)
			} else {
				logrus.SetLevel(logrus.ErrorLevel)
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	flags.AddRootFlags(cmd, ro)

	cmd.AddCommand(cranecmd.NewCmdAuthLogin("hauler"))
	cmd.AddCommand(cranecmd.NewCmdAuthLogout("hauler"))
	addStore(cmd, ro)
	addVersion(cmd, ro)
	addCompletion(cmd, ro)

	return cmd
}
