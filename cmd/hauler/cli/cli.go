package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/internal/flags"
	"github.com/rancherfederal/hauler/pkg/log"
)

var ro = &flags.CliRootOpts{}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hauler",
		Short: "Airgap Swiss Army Knife",
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

	pf := cmd.PersistentFlags()
	pf.StringVarP(&ro.LogLevel, "log-level", "l", "info", "")

	// Add subcommands
	addLogin(cmd)
	addStore(cmd)
	addVersion(cmd)
	addCompletion(cmd)

	return cmd
}
