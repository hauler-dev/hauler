package store

import (
	"context"
	"os"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

// extracts the contents of an archived oci layout to an existing oci layout
func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	for _, fileName := range o.FileName {
		l.Infof("loading haul [%s] to [%s]", o.FileName, o.StoreDir)
		err := unarchiveLayoutTo(ctx, fileName, o.StoreDir, o.TempOverride)
		if err != nil {
			return err
		}
	}

	return nil
}

// unarchiveLayoutTo accepts an archived OCI layout, extracts the contents to an existing OCI layout, and preserves the index
func unarchiveLayoutTo(ctx context.Context, haulPath string, dest string, tempOverride string) error {
	l := log.FromContext(ctx)

	var tempDir string

	if tempOverride != "" {
		tempDir = tempOverride
	} else {

		parent := os.Getenv(consts.HaulerTempDir)
		var err error
		tempDir, err = os.MkdirTemp(parent, consts.DefaultHaulerTempDirName)
		if err != nil {
			return err
		}
		defer os.RemoveAll(tempDir)
	}

	l.Debugf("using temporary directory [%s]", tempDir)

	if err := archives.Unarchive(ctx, haulPath, tempDir); err != nil {
		return err
	}

	s, err := store.NewLayout(tempDir)
	if err != nil {
		return err
	}

	ts, err := content.NewOCI(dest)
	if err != nil {
		return err
	}

	_, err = s.CopyAll(ctx, ts, nil)
	return err
}
