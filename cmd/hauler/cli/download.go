package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/download"
)

func addDownload(parent *cobra.Command) {
	o := &download.Opts{}

	cmd := &cobra.Command{
		Use:     "download",
		Short:   "Download OCI content from a registry and populate it on disk",
		Aliases: []string{"dl"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, arg []string) error {
			ctx := cmd.Context()

			return download.Cmd(ctx, o, arg[0])
		},
	}
	o.AddArgs(cmd)

	parent.AddCommand(cmd)
}
