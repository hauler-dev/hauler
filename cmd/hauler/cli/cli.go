package cli

import (
	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/log"
)

var ro = &flags.CliRootOpts{}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hauler",
		Short:   "Airgap Swiss Army Knife",
		Example: "  View the Docs: https://docs.hauler.dev\n  Environment Variables: HAULER_DIR | HAULER_TEMP_DIR",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l := log.FromContext(cmd.Context())
			l.SetLevel(ro.LogLevel)
			l.Debugf("running cli command [%s]", cmd.CommandPath())
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	flags.AddRootFlags(cmd, ro)

	// Add subcommands
	addLogin(cmd)
	addStore(cmd)
	addVersion(cmd)
	addCompletion(cmd)

	return cmd
}
