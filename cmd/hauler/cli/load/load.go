package load

import (
	"context"

	"github.com/mholt/archiver/v3"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Opts struct{}

// Cmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func Cmd(ctx context.Context, o *Opts, dir string, archiveRefs ...string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler load`")

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
