package store

import (
	"context"
	"os"

	"github.com/mholt/archiver/v3"

	"github.com/hauler-dev/hauler/internal/flags"
	"github.com/hauler-dev/hauler/pkg/content"
	"github.com/hauler-dev/hauler/pkg/log"
	"github.com/hauler-dev/hauler/pkg/store"
)

// LoadCmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func LoadCmd(ctx context.Context, o *flags.LoadOpts, archiveRefs ...string) error {
	l := log.FromContext(ctx)

	for _, archiveRef := range archiveRefs {
		l.Infof("loading content from [%s] to [%s]", archiveRef, o.StoreDir)
		err := unarchiveLayoutTo(ctx, archiveRef, o.StoreDir, o.TempOverride)
		if err != nil {
			return err
		}
	}

	return nil
}

// unarchiveLayoutTo accepts an archived oci layout and extracts the contents to an existing oci layout, preserving the index
func unarchiveLayoutTo(ctx context.Context, archivePath string, dest string, tempOverride string) error {
	tmpdir, err := os.MkdirTemp(tempOverride, "hauler")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	if err := archiver.Unarchive(archivePath, tmpdir); err != nil {
		return err
	}

	s, err := store.NewLayout(tmpdir)
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
