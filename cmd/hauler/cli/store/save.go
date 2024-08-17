package store

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"

	"github.com/hauler-dev/hauler/pkg/log"
)

type SaveOpts struct {
	*RootOpts
	FileName string
}

func (o *SaveOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.FileName, "filename", "f", "haul.tar.zst", "Name of archive")
}

// SaveCmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func SaveCmd(ctx context.Context, o *SaveOpts, outputFile string) error {
	l := log.FromContext(ctx)

	// TODO: Support more formats?
	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	absOutputfile, err := filepath.Abs(outputFile)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(o.StoreDir); err != nil {
		return err
	}

	err = a.Archive([]string{"."}, absOutputfile)
	if err != nil {
		return err
	}

	l.Infof("saved store [%s] -> [%s]", o.StoreDir, absOutputfile)
	return nil
}
