package mapper

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// unpackWriteCloser buffers writes to a temp file, then expands it as tar+gzip into dir on Close.
type unpackWriteCloser struct {
	dir string
	tmp *os.File
}

func newUnpackWriteCloser(dir string) (*unpackWriteCloser, error) {
	tmp, err := os.CreateTemp("", "hauler-unpack")
	if err != nil {
		return nil, err
	}
	return &unpackWriteCloser{dir: dir, tmp: tmp}, nil
}

func (u *unpackWriteCloser) Write(p []byte) (int, error) {
	return u.tmp.Write(p)
}

func (u *unpackWriteCloser) Close() error {
	tmpPath := u.tmp.Name()
	defer os.Remove(tmpPath)

	if err := u.tmp.Close(); err != nil {
		return err
	}

	return extractArchive(tmpPath, u.dir)
}

// extractArchive expands a tar+gzip archive into dir, stripping the archive's own top-level prefix directory.
func extractArchive(archivePath, dir string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("unable to resolve destination dir: %w", err)
	}
	root = filepath.Clean(root)

	f, err := os.Open(archivePath)
	if err != nil {
		return err
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
			return fmt.Errorf("failed to read archive %s: %w", archivePath, err)
		}

		entry := stripTopLevel(hdr.Name)
		entry = filepath.Clean(strings.ReplaceAll(entry, "/", string(filepath.Separator)))
		if entry == "." || entry == "" {
			continue
		}

		// Reject an entry name that resolves outside root, same guard as filestore.go's Push.
		target := filepath.Clean(filepath.Join(root, entry))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
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
				return fmt.Errorf("failed to write %s from archive: %w", target, err)
			}
			out.Close()
		}
	}

	return nil
}

// stripTopLevel drops an archive entry's top-level directory segment.
func stripTopLevel(name string) string {
	for i, c := range name {
		if c == '/' {
			return name[i+1:]
		}
	}
	return ""
}
