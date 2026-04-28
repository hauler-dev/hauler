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

// compressionZstd and archivalTar are package-level vars so tests can reference
// them without importing mholt/archives directly.
var (
	compressionZstd = archives.Zstd{}
	archivalTar     = archives.Tar{}
)

// maps to handle compression types
var CompressionMap = map[string]archives.Compression{
	"gz":  archives.Gz{},
	"bz2": archives.Bz2{},
	"xz":  archives.Xz{},
	"zst": archives.Zstd{},
	"lz4": archives.Lz4{},
	"br":  archives.Brotli{},
}

// maps to handle archival types
var ArchivalMap = map[string]archives.Archival{
	"tar": archives.Tar{},
	"zip": archives.Zip{},
}

// check if a path exists
func isExist(path string) bool {
	_, statErr := os.Stat(path)
	return !os.IsNotExist(statErr)
}

// archives the files in a directory
// dir: the directory to Archive
// outfile: the output file
// compression: the compression to use (gzip, bzip2, etc.)
// archival: the archival to use (tar, zip, etc.)
func Archive(ctx context.Context, dir, outfile string, compression archives.Compression, archival archives.Archival) error {
	l := log.FromContext(ctx)
	l.Debugf("starting the archival process for [%s]", dir)

	// remove outfile
	l.Debugf("removing existing output file: [%s]", outfile)
	if err := os.RemoveAll(outfile); err != nil {
		errMsg := fmt.Errorf("failed to remove existing output file [%s]: %w", outfile, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}

	if !isExist(dir) {
		errMsg := fmt.Errorf("directory [%s] does not exist, cannot proceed with archival", dir)
		l.Debugf(errMsg.Error())
		return errMsg
	}

	// map files on disk to their paths in the archive
	l.Debugf("mapping files in directory [%s]", dir)
	archiveDirName := filepath.Base(filepath.Clean(dir))
	if dir == "." {
		archiveDirName = ""
	}
	files, err := archives.FilesFromDisk(context.Background(), nil, map[string]string{
		dir: archiveDirName,
	})
	if err != nil {
		errMsg := fmt.Errorf("error mapping files from directory [%s]: %w", dir, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}
	l.Debugf("successfully mapped files for directory [%s]", dir)

	// create the output file we'll write to
	l.Debugf("creating output file [%s]", outfile)
	outf, err := os.Create(outfile)
	if err != nil {
		errMsg := fmt.Errorf("error creating output file [%s]: %w", outfile, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}
	defer func() {
		l.Debugf("closing output file [%s]", outfile)
		outf.Close()
	}()

	// define the archive format
	l.Debugf("defining the archive format: [%T]/[%T]", archival, compression)
	format := archives.CompressedArchive{
		Compression: compression,
		Archival:    archival,
	}

	// create the archive
	l.Debugf("starting archive for [%s]", outfile)
	err = format.Archive(context.Background(), outf, files)
	if err != nil {
		errMsg := fmt.Errorf("error during archive creation for output file [%s]: %w", outfile, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}
	l.Debugf("archive created successfully [%s]", outfile)
	return nil
}

// SplitArchive splits an existing archive into chunks of at most maxBytes each.
// Chunks are named <base>_0<ext>, <base>_1<ext>, ... where base is the archive
// path with all extensions stripped, and ext is the compound extension (e.g. .tar.zst).
// The original archive is removed after successful splitting.
func SplitArchive(ctx context.Context, archivePath string, maxBytes int64) ([]string, error) {
	l := log.FromContext(ctx)

	// derive base path and compound extension by stripping all extensions
	base := archivePath
	ext := ""
	for filepath.Ext(base) != "" {
		ext = filepath.Ext(base) + ext
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive for splitting: %w", err)
	}

	var chunks []string
	buf := make([]byte, 32*1024)
	chunkIdx := 0
	var written int64
	var outf *os.File

	for {
		if outf == nil {
			chunkPath := fmt.Sprintf("%s_%d%s", base, chunkIdx, ext)
			outf, err = os.Create(chunkPath)
			if err != nil {
				f.Close()
				return nil, fmt.Errorf("failed to create chunk %d: %w", chunkIdx, err)
			}
			chunks = append(chunks, chunkPath)
			l.Debugf("creating chunk [%s]", chunkPath)
			written = 0
			chunkIdx++
		}

		remaining := maxBytes - written
		readSize := int64(len(buf))
		if readSize > remaining {
			readSize = remaining
		}

		n, readErr := f.Read(buf[:readSize])
		if n > 0 {
			if _, writeErr := outf.Write(buf[:n]); writeErr != nil {
				outf.Close()
				f.Close()
				return nil, fmt.Errorf("failed to write to chunk: %w", writeErr)
			}
			written += int64(n)
		}

		if readErr == io.EOF {
			outf.Close()
			outf = nil
			break
		}
		if readErr != nil {
			outf.Close()
			f.Close()
			return nil, fmt.Errorf("failed to read archive: %w", readErr)
		}

		if written >= maxBytes {
			outf.Close()
			outf = nil
		}
	}

	f.Close()
	if err := os.Remove(archivePath); err != nil {
		return nil, fmt.Errorf("failed to remove original archive after splitting: %w", err)
	}

	l.Infof("split archive [%s] into %d chunk(s)", filepath.Base(archivePath), len(chunks))
	return chunks, nil
}
