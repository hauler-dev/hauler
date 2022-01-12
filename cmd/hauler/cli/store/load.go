package store

import (
	"context"

	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type LoadOpts struct {
	*RootOpts
}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f
}

// LoadCmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func LoadCmd(ctx context.Context, o *LoadOpts, archiveRefs ...string) error {
	l := log.FromContext(ctx)

	// TODO: Support more formats?
	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	for _, archiveRef := range archiveRefs {
		l.Infof("loading content from [%s] to [%s]", archiveRef, o.StoreDir)
		err := a.Unarchive(archiveRef, o.StoreDir)
		if err != nil {
			return err
		}
	}

	return nil
}
