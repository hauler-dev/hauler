package server

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractRepo unpacks the tar+gzip blob getter's directory support produces for a directory source (see pkg/getter/directory.go) back into dir as a plain bare repo layout, since whatever objects/refs/packs the original repo directory had are preserved exactly, so NewGit never needs to understand git's pack format at all.
func ExtractRepo(archivePath, dir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("unable to open git repository archive: %w", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s is not a gzip-compressed archive: %w", archivePath, err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read git repository archive %s: %w", archivePath, err)
		}

		target := filepath.Join(dir, stripTopLevel(hdr.Name))
		if target == dir {
			continue
		}

		// Reject an entry name that resolves outside dir, same guard as internal/mapper's Push.
		rel, err := filepath.Rel(dir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("path_traversal_disallowed: %q resolves outside destination dir", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("failed to write %s from git repository archive: %w", target, err)
			}
			out.Close()
		}
	}

	return nil
}

// stripTopLevel drops the archive's single top-level directory (the repo's own name, added by tarDir's prefix) so dir itself becomes the repo root rather than dir/<name>/....
func stripTopLevel(name string) string {
	for i, c := range name {
		if c == '/' {
			return name[i+1:]
		}
	}
	return ""
}
