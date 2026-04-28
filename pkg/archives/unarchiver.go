package archives

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/mholt/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
)

// extractionLimits holds per-extraction caps to prevent decompression bombs
// and file-count exhaustion attacks.
type extractionLimits struct {
	maxFileBytes  int64 // maximum bytes for a single extracted file
	maxTotalBytes int64 // maximum aggregate bytes across all extracted files
	maxFiles      int64 // maximum number of files extracted
}

const (
	dirPermissions  = 0o700 // default directory permissions
	filePermissions = 0o600 // default file permissions
)

// ensures the path is safely relative to the target directory
func securePath(basePath, relativePath string) (string, error) {
	relativePath = filepath.Clean("/" + relativePath)
	relativePath = strings.TrimPrefix(relativePath, string(os.PathSeparator))

	dstPath := filepath.Join(basePath, relativePath)

	if !strings.HasPrefix(filepath.Clean(dstPath)+string(os.PathSeparator), filepath.Clean(basePath)+string(os.PathSeparator)) {
		return "", fmt.Errorf("illegal file path: %s", dstPath)
	}
	return dstPath, nil
}

// creates a directory with specified permissions
func createDirWithPermissions(ctx context.Context, path string, mode os.FileMode) error {
	l := log.FromContext(ctx)
	l.Debugf("creating directory [%s]", path)
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("failed to mkdir: %w", err)
	}
	return nil
}

// sets permissions to a file or directory
func setPermissions(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("failed to chmod: %w", err)
	}
	return nil
}

// extractionState tracks mutable counters shared across handleFile calls.
// archiveSize is the on-disk size of the input archive; when non-zero it is
// used to enforce a decompression-ratio bomb check against totalBytes.
type extractionState struct {
	totalBytes  atomic.Int64
	fileCount   atomic.Int64
	archiveSize int64
}

// handles the extraction of a file from the archive.
func handleFileWithLimits(ctx context.Context, f archives.FileInfo, dst string, lim extractionLimits, state *extractionState) error {
	l := log.FromContext(ctx)
	l.Debugf("handling file [%s]", f.NameInArchive)

	// validate and construct the destination path
	dstPath, pathErr := securePath(dst, f.NameInArchive)
	if pathErr != nil {
		return pathErr
	}

	// ensure the parent directory exists
	parentDir := filepath.Dir(dstPath)
	if dirErr := createDirWithPermissions(ctx, parentDir, dirPermissions); dirErr != nil {
		return dirErr
	}

	// handle directories
	if f.IsDir() {
		if dirErr := createDirWithPermissions(ctx, dstPath, f.Mode()); dirErr != nil {
			return fmt.Errorf("failed to create directory: %w", dirErr)
		}
		l.Debugf("successfully created directory [%s]", dstPath)
		return nil
	}

	// ignore symlinks (or hardlinks)
	if f.LinkTarget != "" {
		l.Debugf("skipping symlink [%s] to [%s]", dstPath, f.LinkTarget)
		return nil
	}

	// enforce file count cap
	if state != nil {
		count := state.fileCount.Add(1)
		if count > lim.maxFiles {
			return fmt.Errorf("archive contains more than %d files (extraction limit exceeded)", lim.maxFiles)
		}
	}

	// check and handle parent directory permissions
	originalMode, statErr := os.Stat(parentDir)
	if statErr != nil {
		return fmt.Errorf("failed to stat parent directory: %w", statErr)
	}

	// if parent directory is read only, temporarily make it writable
	if originalMode.Mode().Perm()&0o200 == 0 {
		l.Debugf("parent directory is read only... temporarily making it writable [%s]", parentDir)
		if chmodErr := os.Chmod(parentDir, originalMode.Mode()|0o200); chmodErr != nil {
			return fmt.Errorf("failed to chmod parent directory: %w", chmodErr)
		}
		defer func() {
			if chmodErr := os.Chmod(parentDir, originalMode.Mode()); chmodErr != nil {
				l.Debugf("failed to restore original permissions for [%s]: %v", parentDir, chmodErr)
			}
		}()
	}

	// handle regular files
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

	// copy with per-file and aggregate byte caps
	var totalPtr *atomic.Int64
	var archiveSize int64
	if state != nil {
		totalPtr = &state.totalBytes
		archiveSize = state.archiveSize
	}
	written, copyErr := copyBounded(dstFile, reader, lim.maxFileBytes, lim.maxTotalBytes, totalPtr, archiveSize)
	if copyErr != nil {
		return fmt.Errorf("failed to copy: %w", copyErr)
	}
	_ = written
	l.Debugf("successfully extracted file [%s]", dstPath)
	return nil
}

// copyBounded copies from src to dst, enforcing a per-file cap (maxFile) and
// updating a shared total counter checked against maxTotal.  total may be nil
// when called from the default (non-tracked) code path.  When archiveSize > 0,
// it also enforces a decompression-ratio cap against consts.MaxDecompressionRatio.
func copyBounded(dst io.Writer, src io.Reader, maxFile, maxTotal int64, total *atomic.Int64, archiveSize int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var fileWritten int64
	for {
		nr, readErr := src.Read(buf)
		if nr > 0 {
			fileWritten += int64(nr)
			if fileWritten > maxFile {
				return fileWritten, fmt.Errorf("extracted file exceeds per-file size limit of %d bytes", maxFile)
			}
			if total != nil {
				agg := total.Add(int64(nr))
				if agg > maxTotal {
					return fileWritten, fmt.Errorf("total extracted bytes exceed aggregate limit of %d bytes", maxTotal)
				}
				if archiveSize > 0 && float64(agg) > float64(archiveSize)*consts.MaxDecompressionRatio {
					return fileWritten, fmt.Errorf("decompression ratio exceeds %vx (likely zip bomb)", consts.MaxDecompressionRatio)
				}
			}
			if _, writeErr := dst.Write(buf[:nr]); writeErr != nil {
				return fileWritten, writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fileWritten, readErr
		}
	}
	return fileWritten, nil
}

// handleFile delegates to handleFileWithLimits using the default (production) limits.
// It exists so the Unarchive function signature remains unchanged.
func handleFile(ctx context.Context, f archives.FileInfo, dst string, state *extractionState) error {
	// The production limits are extremely generous and should never fire on
	// legitimate hauler workloads; they exist to bound pathological inputs.
	defaultLimits := extractionLimits{
		maxFileBytes:  consts.MaxArchiveFileBytes,
		maxTotalBytes: consts.MaxArchiveBytes,
		maxFiles:      consts.MaxArchiveFiles,
	}
	return handleFileWithLimits(ctx, f, dst, defaultLimits, state)
}

// unarchives a tarball to a directory, symlinks, and hardlinks are ignored
func Unarchive(ctx context.Context, tarball, dst string) error {
	l := log.FromContext(ctx)
	l.Debugf("unarchiving temporary archive [%s] to temporary store [%s]", tarball, dst)
	archiveFile, openErr := os.Open(tarball)
	if openErr != nil {
		return fmt.Errorf("failed to open tarball %s: %w", tarball, openErr)
	}
	defer archiveFile.Close()

	stat, statErr := archiveFile.Stat()
	if statErr != nil {
		return fmt.Errorf("failed to stat tarball %s: %w", tarball, statErr)
	}

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

	state := &extractionState{archiveSize: stat.Size()}
	handler := func(ctx context.Context, f archives.FileInfo) error {
		return handleFile(ctx, f, dst, state)
	}

	if extractErr := extractor.Extract(context.Background(), input, handler); extractErr != nil {
		return fmt.Errorf("failed to extract: %w", extractErr)
	}

	l.Infof("unarchiving completed successfully")
	return nil
}

// unarchiveWithLimits is like Unarchive but enforces explicit extraction limits.
// It is used directly in tests to verify cap enforcement with small values.
func unarchiveWithLimits(ctx context.Context, tarball, dst string, lim extractionLimits) error {
	l := log.FromContext(ctx)
	l.Debugf("unarchiving [%s] to [%s] with limits", tarball, dst)

	archiveFile, openErr := os.Open(tarball)
	if openErr != nil {
		return fmt.Errorf("failed to open tarball %s: %w", tarball, openErr)
	}
	defer archiveFile.Close()

	stat, statErr := archiveFile.Stat()
	if statErr != nil {
		return fmt.Errorf("failed to stat tarball %s: %w", tarball, statErr)
	}

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

	state := &extractionState{archiveSize: stat.Size()}
	handler := func(ctx context.Context, f archives.FileInfo) error {
		return handleFileWithLimits(ctx, f, dst, lim, state)
	}

	if extractErr := extractor.Extract(context.Background(), input, handler); extractErr != nil {
		return fmt.Errorf("failed to extract: %w", extractErr)
	}
	return nil
}

var chunkSuffixRe = regexp.MustCompile(`^(.+)_(\d+)$`)

// chunkInfo checks whether archivePath matches the chunk naming pattern (<base>_N<ext>).
// Returns the base path (without index), compound extension, numeric index, and whether it matched.
func chunkInfo(archivePath string) (base, ext string, index int, ok bool) {
	dir := filepath.Dir(archivePath)
	name := filepath.Base(archivePath)

	// strip compound extension (e.g. .tar.zst)
	nameBase := name
	nameExt := ""
	for filepath.Ext(nameBase) != "" {
		nameExt = filepath.Ext(nameBase) + nameExt
		nameBase = strings.TrimSuffix(nameBase, filepath.Ext(nameBase))
	}

	m := chunkSuffixRe.FindStringSubmatch(nameBase)
	if m == nil {
		return "", "", 0, false
	}

	idx, _ := strconv.Atoi(m[2])
	return filepath.Join(dir, m[1]), nameExt, idx, true
}

// JoinChunks detects whether archivePath is a chunk file and, if so, finds all
// sibling chunks, concatenates them in numeric order into a single file in tempDir,
// and returns the path to the joined file. If archivePath is not a chunk, it is
// returned unchanged.
func JoinChunks(ctx context.Context, archivePath, tempDir string) (string, error) {
	l := log.FromContext(ctx)

	base, ext, _, ok := chunkInfo(archivePath)
	if !ok {
		return archivePath, nil
	}

	all, err := filepath.Glob(base + "_*" + ext)
	if err != nil {
		return archivePath, nil
	}
	var matches []string
	for _, m := range all {
		if _, _, _, ok := chunkInfo(m); ok {
			matches = append(matches, m)
		}
	}
	if len(matches) == 0 {
		return archivePath, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		_, _, idxI, _ := chunkInfo(matches[i])
		_, _, idxJ, _ := chunkInfo(matches[j])
		return idxI < idxJ
	})

	l.Debugf("joining %d chunk(s) for [%s]", len(matches), base)

	joinedPath := filepath.Join(tempDir, filepath.Base(base)+ext)
	outf, err := os.Create(joinedPath)
	if err != nil {
		return "", fmt.Errorf("failed to create joined archive: %w", err)
	}
	defer outf.Close()

	var joinedTotal int64
	for _, chunk := range matches {
		l.Debugf("joining chunk [%s]", chunk)
		cf, err := os.Open(chunk)
		if err != nil {
			return "", fmt.Errorf("failed to open chunk [%s]: %w", chunk, err)
		}
		remaining := consts.MaxArchiveBytes - joinedTotal
		if remaining <= 0 {
			cf.Close()
			return "", fmt.Errorf("joined chunks exceed maximum allowed size (%d bytes)", consts.MaxArchiveBytes)
		}
		n, err := io.Copy(outf, io.LimitReader(cf, remaining+1))
		cf.Close()
		if err != nil {
			return "", fmt.Errorf("failed to copy chunk [%s]: %w", chunk, err)
		}
		joinedTotal += n
		if joinedTotal > consts.MaxArchiveBytes {
			return "", fmt.Errorf("joined chunks exceed maximum allowed size (%d bytes)", consts.MaxArchiveBytes)
		}
	}

	l.Infof("joined %d chunk(s) into [%s]", len(matches), filepath.Base(joinedPath))
	return joinedPath, nil
}
