package store

import (
	"context"

	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type LoadOpts struct {
	OutputDir string
}

func (o *LoadOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.OutputDir, "output", "o", "", "Directory to unload archived contents to (defaults to $PWD/haul)")
}

// LoadCmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func LoadCmd(ctx context.Context, o *LoadOpts, dir string, archiveRefs ...string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler store load`")

	// TODO: Support more formats?
	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	odir := dir
	if o.OutputDir != "" {
		odir = o.OutputDir
	}

	for _, archiveRef := range archiveRefs {
		l.Infof("loading content from [%s] to [%s]", archiveRef, odir)
		err := a.Unarchive(archiveRef, odir)
		if err != nil {
			return err
		}
	}

	return nil
}
