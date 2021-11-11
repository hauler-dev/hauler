package str

import (
	"context"

	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type LoadOpts struct{}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f
}

// LoadCmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func LoadCmd(ctx context.Context, o *LoadOpts, dir string, archiveRefs ...string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler store load`")

	// TODO: Support more formats?
	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	for _, archiveRef := range archiveRefs {
		l.Infof("Loading content from %s to %s", archiveRef, dir)
		err := a.Unarchive(archiveRef, dir)
		if err != nil {
			return err
		}
	}

	return nil
}
