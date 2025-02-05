package store

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/getter"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

// extracts the contents of an archived oci layout to an existing oci layout
func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	tempOverride := o.TempOverride

	if tempOverride == "" {
		tempOverride = os.Getenv(consts.HaulerTempDir)
	}

	tempDir, err := os.MkdirTemp(tempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	l.Debugf("using temporary directory at [%s]", tempDir)

	for _, fileName := range o.FileName {
		l.Infof("loading haul [%s] to [%s]", fileName, o.StoreDir)
		err := unarchiveLayoutTo(ctx, fileName, o.StoreDir, tempDir)
		if err != nil {
			return err
		}
	}

	return nil
}

// accepts an archived OCI layout, extracts the contents to an existing OCI layout, and preserves the index
func unarchiveLayoutTo(ctx context.Context, haulPath string, dest string, tempDir string) error {
	l := log.FromContext(ctx)

	if strings.HasPrefix(haulPath, "http://") || strings.HasPrefix(haulPath, "https://") {
		l.Debugf("detected remote archive... starting download... [%s]", haulPath)

		h := getter.NewHttp()
		parsedURL, err := url.Parse(haulPath)
		if err != nil {
			return err
		}
		rc, err := h.Open(ctx, parsedURL)
		if err != nil {
			return err
		}
		defer rc.Close()

		fileName := h.Name(parsedURL)
		if fileName == "" {
			fileName = filepath.Base(parsedURL.Path)
		}
		haulPath = filepath.Join(tempDir, fileName)

		out, err := os.Create(haulPath)
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err = io.Copy(out, rc); err != nil {
			return err
		}
	}

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
