package archives

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mholt/archives"
	"hauler.dev/go/hauler/pkg/log"
)

const (
	dirPermissions  = 0o700 // Default directory permissions
	filePermissions = 0o600 // Default file permissions
)

// securePath ensures the path is safely relative to the target directory.
func securePath(basePath, relativePath string) (string, error) {
	relativePath = filepath.Clean("/" + relativePath)                         // Normalize path with a leading slash
	relativePath = strings.TrimPrefix(relativePath, string(os.PathSeparator)) // Remove leading separator

	dstPath := filepath.Join(basePath, relativePath)

	if !strings.HasPrefix(filepath.Clean(dstPath)+string(os.PathSeparator), filepath.Clean(basePath)+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal file path: %s", dstPath)
	}
	return dstPath, nil
}

// createDirWithPermissions creates a directory with specified permissions.
func createDirWithPermissions(ctx context.Context, path string, mode os.FileMode) error {
	l := log.FromContext(ctx)
	l.Debugf("creating directory [%s]", path)
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("failed to mkdir: %w", err)
	}
	return nil
}

// setPermissions applies permissions to a file or directory.
func setPermissions(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("failed to chmod: %w", err)
	}
	return nil
}

// handleFile handles the extraction of a file from the archive.
func handleFile(ctx context.Context, f archives.FileInfo, dst string) error {
	l := log.FromContext(ctx)
	l.Debugf("handling file [%s]", f.NameInArchive)

	// Validate and construct the destination path
	dstPath, pathErr := securePath(dst, f.NameInArchive)
	if pathErr != nil {
		return pathErr
	}

	// Ensure the parent directory exists
	parentDir := filepath.Dir(dstPath)
	if dirErr := createDirWithPermissions(ctx, parentDir, dirPermissions); dirErr != nil {
		return dirErr
	}

	// Handle directories
	if f.IsDir() {
		// Create the directory with permissions from the archive
		if dirErr := createDirWithPermissions(ctx, dstPath, f.Mode()); dirErr != nil {
			return fmt.Errorf("failed to create directory: %w", dirErr)
		}
		l.Debugf("successfully created directory [%s]", dstPath)
		return nil
	}

	// Ignore symlinks (or hardlinks)
	if f.LinkTarget != "" {
		l.Debugf("skipping symlink [%s] to [%s]", dstPath, f.LinkTarget)
		return nil
	}

	// Check and handle parent directory permissions
	originalMode, statErr := os.Stat(parentDir)
	if statErr != nil {
		return fmt.Errorf("failed to stat parent directory: %w", statErr)
	}

	// If parent directory is read-only, temporarily make it writable
	if originalMode.Mode().Perm()&0o200 == 0 {
		l.Debugf("parent directory is read only... temporarily making it writable [%s]", parentDir)
		if chmodErr := os.Chmod(parentDir, originalMode.Mode()|0o200); chmodErr != nil {
			return fmt.Errorf("failed to chmod parent directory: %w", chmodErr)
		}
		defer func() {
			// Restore the original permissions after writing
			if chmodErr := os.Chmod(parentDir, originalMode.Mode()); chmodErr != nil {
				l.Debugf("failed to restore original permissions for [%s]: %v", parentDir, chmodErr)
			}
		}()
	}

	// Handle regular files
	reader, openErr := f.Open()
	if openErr != nil {
		return fmt.Errorf("failed to open file: %w", openErr)
	}
	defer reader.Close()

	dstFile, createErr := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY, f.Mode())
	if createErr != nil {
		return fmt.Errorf("failed to create file: %w", createErr)
	}
	defer dstFile.Close()

	if _, copyErr := io.Copy(dstFile, reader); copyErr != nil {
		return fmt.Errorf("failed to copy: %w", copyErr)
	}
	l.Debugf("successfully extracted file [%s]", dstPath)
	return nil
}

// Unarchive unarchives a tarball to a directory, symlinks and hardlinks are ignored.
func Unarchive(ctx context.Context, tarball, dst string) error {
	l := log.FromContext(ctx)
	l.Debugf("unarchiving temporary archive [%s] to temporary store [%s]", tarball, dst)
	archiveFile, openErr := os.Open(tarball)
	if openErr != nil {
		return fmt.Errorf("failed to open tarball %s: %w", tarball, openErr)
	}
	defer archiveFile.Close()

	format, input, identifyErr := archives.Identify(context.Background(), tarball, archiveFile)
	if identifyErr != nil {
		return fmt.Errorf("failed to identify format: %w", identifyErr)
	}

	extractor, ok := format.(archives.Extractor)
	if !ok {
		return fmt.Errorf("unsupported format for extraction")
	}

	if dirErr := createDirWithPermissions(ctx, dst, dirPermissions); dirErr != nil {
		return fmt.Errorf("failed to create destination directory: %w", dirErr)
	}

	handler := func(ctx context.Context, f archives.FileInfo) error {
		return handleFile(ctx, f, dst)
	}

	if extractErr := extractor.Extract(context.Background(), input, handler); extractErr != nil {
		return fmt.Errorf("failed to extract: %w", extractErr)
	}

	l.Infof("unarchiving completed successfully")
	return nil
}
