package cli

import (
	"context"

	cranecmd "github.com/google/go-containerregistry/cmd/crane/cmd"
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
)

func New(ctx context.Context, ro *flags.CliRootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hauler",
		Short:   "Airgap Swiss Army Knife",
		Example: "  View the Docs: https://docs.hauler.dev\n  Environment Variables: " + consts.HaulerDir + " | " + consts.HaulerTempDir + " | " + consts.HaulerStoreDir + " | " + consts.HaulerIgnoreErrors,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l := log.FromContext(ctx)
			l.SetLevel(ro.LogLevel)
			l.Debugf("running cli command [%s]", cmd.CommandPath())

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
