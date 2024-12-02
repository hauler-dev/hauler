package cli

import (
	"context"
	"embed"

	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/log"
)

func New(ctx context.Context, binaries embed.FS, ro *flags.CliRootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "hauler",
		Short:   "Airgap Swiss Army Knife",
		Example: "  View the Docs: https://docs.hauler.dev\n  Environment Variables: " + consts.HaulerDir + " | " + consts.HaulerTempDir + " | " + consts.HaulerIgnoreErrors,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l := log.FromContext(ctx)
			l.SetLevel(ro.LogLevel)
			l.Debugf("running cli command [%s]", cmd.CommandPath())

			// ensure cosign binary is available
			if err := cosign.EnsureBinaryExists(ctx, binaries, ro); err != nil {
				l.Errorf("cosign binary missing: %v", err)
				return err
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	flags.AddRootFlags(cmd, ro)

	addLogin(cmd, ro)
	addStore(cmd, ro)
	addVersion(cmd, ro)
	addCompletion(cmd, ro)

	return cmd
}
