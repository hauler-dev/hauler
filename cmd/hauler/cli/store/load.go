package store

import (
	"context"
	"os"

	"github.com/mholt/archiver/v3"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
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
	if tempOverride == "" {
		tempOverride = os.Getenv("HAULER_TEMP_DIR")
	}

	tempDir, err := os.MkdirTemp(tempOverride, "hauler")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	if err := archiver.Unarchive(archivePath, tempDir); err != nil {
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
