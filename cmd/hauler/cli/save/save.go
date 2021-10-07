package save

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Opts struct {
	FileName string
}

func (o *Opts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.FileName, "filename", "f", "pkg.tar.zst", "Name of archive")
}

// Cmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func Cmd(ctx context.Context, o *Opts, outputFile string, dir string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler save`")

	// TODO: Support more formats?
	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	absOutputfile, err := filepath.Abs(outputFile)
	if err != nil {
		return err
	}

	l.Infof("Saving data dir (%s) as compressed archive to %s", dir, absOutputfile)
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		return err
	}

	err = a.Archive([]string{"."}, absOutputfile)
	if err != nil {
		return err
	}

	return nil
}
