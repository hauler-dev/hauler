package mapper

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mholt/archives"
	digest "github.com/opencontainers/go-digest"

	"hauler.dev/go/hauler/v2/pkg/log"
)

// unpackWriteCloser buffers writes to a temp file, then on Close verifies the digest and expands the compressed tar into dir.
type unpackWriteCloser struct {
	ctx      context.Context
	dir      string
	expected digest.Digest
	digester digest.Digester
	tmp      *os.File
}

func newUnpackWriteCloser(ctx context.Context, dir string, expected digest.Digest) (*unpackWriteCloser, error) {
	tmp, err := os.CreateTemp("", "hauler-unpack")
	if err != nil {
		return nil, err
	}
	return &unpackWriteCloser{ctx: ctx, dir: dir, expected: expected, digester: digest.Canonical.Digester(), tmp: tmp}, nil
}

func (u *unpackWriteCloser) Write(p []byte) (int, error) {
	n, err := u.tmp.Write(p)
	if n > 0 {
		u.digester.Hash().Write(p[:n])
	}
	return n, err
}

func (u *unpackWriteCloser) Close() error {
	tmpPath := u.tmp.Name()
	defer os.Remove(tmpPath)

	if err := u.tmp.Close(); err != nil {
		return err
	}

	// Never touch the destination with content that failed verification.
	if u.expected != "" {
		if got := u.digester.Digest(); got != u.expected {
			return fmt.Errorf("digest mismatch for directory archive... expected [%s] but got [%s]", u.expected, got)
		}
	}

	return replaceDir(u.ctx, tmpPath, u.dir)
}

// replaceDir extracts into a staging sibling of dir and swaps it in only once extraction fully succeeds.
func replaceDir(ctx context.Context, archivePath, dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory for [%s]: %w", dir, err)
	}

	staging, err := os.MkdirTemp(parent, "."+filepath.Base(dir)+".hauler-extract-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory for [%s]: %w", dir, err)
	}

	if err := extractArchive(ctx, archivePath, staging); err != nil {
		os.RemoveAll(staging)
		return err
	}

	if err := os.RemoveAll(dir); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("failed to clear destination directory [%s]: %w", dir, err)
	}
	if err := os.Rename(staging, dir); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("failed to move extracted directory into [%s]: %w", dir, err)
	}
	return nil
}

// openTarStream identifies the compression from the content, so both tar.zst layers and older tar.gz layers extract.
func openTarStream(ctx context.Context, r io.Reader) (io.ReadCloser, error) {
	identified, input, err := archives.Identify(ctx, "", r)
	if err != nil {
		return nil, err
	}

	switch fm := identified.(type) {
	case archives.CompressedArchive:
		if fm.Compression == nil {
			return io.NopCloser(input), nil
		}
		return fm.Compression.OpenReader(input)
	case archives.Compression:
		return fm.OpenReader(input)
	case archives.Tar:
		return io.NopCloser(input), nil
	default:
		return nil, fmt.Errorf("unsupported archive type [%T]", identified)
	}
}

// dirMeta is a directory's mode and mtime, applied only after everything inside it has been written.
type dirMeta struct {
	path    string
	mode    os.FileMode
	modTime time.Time
}

// preservedModeBits are the permission bits restored on extract, including setuid, setgid, and sticky.
const preservedModeBits = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// extractArchive expands a compressed tar into dir, stripping the archive's own top-level prefix directory.
func extractArchive(ctx context.Context, archivePath, dir string) error {
	l := log.FromContext(ctx)

	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve destination directory: %w", err)
	}
	root = filepath.Clean(root)

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	zr, err := openTarStream(ctx, f)
	if err != nil {
		return fmt.Errorf("unable to read [%s] as a tar archive: %w", archivePath, err)
	}
	defer zr.Close()

	var dirs []dirMeta
	var links []*tar.Header

	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read archive [%s]: %w", archivePath, err)
		}

		// Reject an entry name that resolves outside root, same guard as filestore.go's Push.
		target := entryTarget(root, hdr.Name)
		if !within(root, target) {
			return fmt.Errorf("path_traversal_disallowed: %q resolves outside destination dir", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			dirs = append(dirs, dirMeta{path: target, mode: hdr.FileInfo().Mode() & preservedModeBits, modTime: hdr.ModTime})
		case tar.TypeReg:
			if target == root {
				return fmt.Errorf("archive entry [%s] is a file at the destination root", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := writeFile(tr, target, hdr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Deferred so no regular file is ever written through a symlink created from this same archive.
			links = append(links, hdr)
		default:
			l.Warnf("skipping archive entry [%s] with unsupported type [%d]", hdr.Name, hdr.Typeflag)
		}
	}

	for _, hdr := range links {
		if err := writeSymlink(root, hdr); err != nil {
			l.Warnf("skipping symlink [%s]: %v", hdr.Name, err)
		}
	}

	// Deepest first so a read-only parent never blocks fixing up its children.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if err := os.Chmod(d.path, d.mode); err != nil {
			return err
		}
		if !d.modTime.IsZero() {
			if err := os.Chtimes(d.path, d.modTime, d.modTime); err != nil {
				return err
			}
		}
	}

	return nil
}

// writeFile writes one regular file entry, then restores its exact mode (bypassing umask) and mtime.
func writeFile(r io.Reader, target string, hdr *tar.Header) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return fmt.Errorf("failed to write [%s] from archive: %w", target, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(target, hdr.FileInfo().Mode()&preservedModeBits); err != nil {
		return err
	}
	if !hdr.ModTime.IsZero() {
		if err := os.Chtimes(target, hdr.ModTime, hdr.ModTime); err != nil {
			return err
		}
	}
	return nil
}

// writeSymlink recreates a symlink only if it is relative, resolves inside root, and no parent path component is itself a symlink.
func writeSymlink(root string, hdr *tar.Header) error {
	target := entryTarget(root, hdr.Name)
	if target == root {
		return fmt.Errorf("symlink cannot replace the destination root")
	}

	link := filepath.FromSlash(hdr.Linkname)
	if link == "" || filepath.IsAbs(link) {
		return fmt.Errorf("target [%s] must be a relative path", hdr.Linkname)
	}
	if resolved := filepath.Clean(filepath.Join(filepath.Dir(target), link)); !within(root, resolved) {
		return fmt.Errorf("target [%s] resolves outside the extracted directory", hdr.Linkname)
	}

	// A symlinked parent would make the lexical check above meaningless, so refuse to nest links under links.
	for p := filepath.Dir(target); p != root; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent [%s] is itself a symlink", p)
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.Symlink(link, target)
}

// within reports whether target is root or inside it.
func within(root, target string) bool {
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}

// entryTarget maps an archive entry name to its path under root, dropping the archive's own top-level directory.
func entryTarget(root, name string) string {
	_, rest, _ := strings.Cut(name, "/")
	return filepath.Join(root, filepath.FromSlash(rest))
}
