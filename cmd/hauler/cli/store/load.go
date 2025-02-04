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
	"hauler.dev/go/hauler/pkg/artifacts/file/getter"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

// loads a content store from one or more store archives
func LoadCmd(ctx context.Context, o *flags.LoadOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, fileNames ...string) error {
	l := log.FromContext(ctx)

	for _, fileName := range fileNames {
		l.Infof("loading archive [%s] to store [%s]", fileName, o.StoreDir)
		if err := unarchiveLayoutTo(ctx, fileName, o.StoreDir, o.TempOverride); err != nil {
			return err
		}
	}

	return nil
}

// unarchiveLayoutTo accepts an archived OCI layout, extracts the contents to an existing OCI layout, and preserves the index
func unarchiveLayoutTo(ctx context.Context, archivePath string, dest string, tempOverride string) error {
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

	// if archivePath detects a remote URL... download it
	if strings.HasPrefix(archivePath, "http://") || strings.HasPrefix(archivePath, "https://") {
		l.Debugf("detected remote archive... starting download... [%s]", archivePath)
		var err error
		archivePath, err = downloadRemote(ctx, archivePath, tempDir)
		if err != nil {
			return err
		}
	}

	if err := archives.Unarchive(ctx, archivePath, tempDir); err != nil {
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

// downloadRemote downloads the remote file using the existing getter
func downloadRemote(ctx context.Context, remoteURL, tempDirDest string) (string, error) {
	parsedURL, err := url.Parse(remoteURL)
	if err != nil {
		return "", err
	}
	h := getter.NewHttp()
	rc, err := h.Open(ctx, parsedURL)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	fileName := h.Name(parsedURL)
	if fileName == "" {
		fileName = filepath.Base(parsedURL.Path)
	}

	localPath := filepath.Join(tempDirDest, fileName)
	out, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	if _, err = io.Copy(out, rc); err != nil {
		return "", err
	}

	return localPath, nil
}
