package store

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/download"
	"github.com/rancherfederal/hauler/pkg/store"
)

type ExtractOpts struct {
	DestinationDir string
}

func (o *ExtractOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
}

func ExtractCmd(ctx context.Context, o *ExtractOpts, s *store.Store, reference string) error {
	eref, err := store.RelocateReference(reference, "")
	if err != nil {
		return err
	}

	gopts := &download.Opts{
		DestinationDir: o.DestinationDir,
	}

	return download.Cmd(ctx, gopts, eref.Name())
}
