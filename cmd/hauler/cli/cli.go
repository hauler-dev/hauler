package cli

import (
	"github.com/spf13/cobra"

	"github.com/hauler-dev/hauler/pkg/log"
)

type rootOpts struct {
	logLevel string
}

var ro = &rootOpts{}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hauler",
		Short: "Airgap Swiss Army Knife",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l := log.FromContext(cmd.Context())
			l.SetLevel(ro.logLevel)
			l.Debugf("running cli command [%s]", cmd.CommandPath())
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&ro.logLevel, "log-level", "l", "info", "")

	// Add subcommands
	addLogin(cmd)
	addStore(cmd)
	addVersion(cmd)
	addCompletion(cmd)

	return cmd
}
