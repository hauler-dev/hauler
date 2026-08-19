package archives

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/mholt/archives"
	"hauler.dev/go/hauler/v2/pkg/log"
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

	if !isExist(dir) {
		errMsg := fmt.Errorf("directory [%s] does not exist, cannot proceed with archival", dir)
		l.Debugf("%s", errMsg.Error())
		return errMsg
	}

	// FilesFromDisk maps a directory key to the directory's *contents*
	// nested one level under the map value: {dir: "blobs"} yields an archive
	// tree rooted at "blobs/<dir's children>", not "blobs/<dir's basename>/...".
	// So the source dir's basename must be supplied explicitly as the map
	// value to reproduce Archive's historical top-level-named layout.
	archiveDirName := filepath.Base(filepath.Clean(dir))
	if dir == "." {
		archiveDirName = ""
	}
	return ArchiveFiles(ctx, map[string]string{dir: archiveDirName}, outfile, compression, archival)
}

// ArchiveFiles archives an explicit disk-path -> archive-path map rather than a
// whole directory, so save can substitute generated files (filtered index.json,
// manifest.json) without ever mutating the source store directory (#744).
func ArchiveFiles(ctx context.Context, fileMap map[string]string, outfile string, compression archives.Compression, archival archives.Archival) error {
	l := log.FromContext(ctx)

	// remove outfile
	l.Debugf("removing existing output file: [%s]", outfile)
	if err := os.RemoveAll(outfile); err != nil {
		errMsg := fmt.Errorf("failed to remove existing output file [%s]: %w", outfile, err)
		l.Debugf("%s", errMsg.Error())
		return errMsg
	}

	// map files on disk to their paths in the archive
	l.Debugf("mapping files for archive [%s]", outfile)
	// mholt/archives.FilesFromDisk ranges over fileMap directly, and Go
	// randomizes map iteration order per range -- even two ranges over the same
	// map in one process can differ. Left alone, that made two saves of an
	// identical store produce byte-different .tar.zst files and shifted
	// --chunk-size boundaries. Each root is walked independently (no shared
	// state across roots in FilesFromDisk), so calling it once per sorted key
	// and concatenating gives the same []FileInfo the library would produce,
	// just in a fixed order.
	keys := make([]string, 0, len(fileMap))
	for k := range fileMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var files []archives.FileInfo
	for _, rootOnDisk := range keys {
		rootFiles, err := archives.FilesFromDisk(ctx, nil, map[string]string{rootOnDisk: fileMap[rootOnDisk]})
		if err != nil {
			errMsg := fmt.Errorf("error mapping files: %w", err)
			l.Debugf("%s", errMsg.Error())
			return errMsg
		}
		files = append(files, rootFiles...)
	}
	l.Debugf("successfully mapped files for archive [%s]", outfile)

	// create the output file we'll write to
	l.Debugf("creating output file [%s]", outfile)
	outf, err := os.Create(outfile)
	if err != nil {
		errMsg := fmt.Errorf("error creating output file [%s]: %w", outfile, err)
		l.Debugf("%s", errMsg.Error())
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
	err = format.Archive(ctx, outf, files)
	if err != nil {
		errMsg := fmt.Errorf("error during archive creation for output file [%s]: %w", outfile, err)
		l.Debugf("%s", errMsg.Error())
		return errMsg
	}
	l.Debugf("archive created successfully [%s]", outfile)
	return nil
}

// splits an existing archive into chunks of at most maxBytes each, named
// <archivePath>.001, .002, ... and removes the original archive afterward.
func SplitArchive(ctx context.Context, archivePath string, maxBytes int64) ([]string, error) {
	l := log.FromContext(ctx)

	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes must be greater than zero, received %d", maxBytes)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open archive for splitting: %w", err)
	}

	var chunks []string
	buf := make([]byte, 32*1024)
	chunkIdx := 1
	var written int64
	var outf *os.File

	for {
		remaining := maxBytes - written
		readSize := int64(len(buf))
		if readSize > remaining {
			readSize = remaining
		}

		n, readErr := f.Read(buf[:readSize])
		if n > 0 {
			// chunk files are only created once there's real data to write,
			// so an archive size that's an exact multiple of maxBytes never
			// leaves a trailing empty chunk behind.
			if outf == nil {
				chunkPath := fmt.Sprintf("%s.%03d", archivePath, chunkIdx)
				outf, err = os.Create(chunkPath)
				if err != nil {
					f.Close()
					return nil, fmt.Errorf("failed to create chunk %d: %w", chunkIdx, err)
				}
				chunks = append(chunks, chunkPath)
				l.Debugf("creating chunk [%s]", chunkPath)
				chunkIdx++
			}
			if _, writeErr := outf.Write(buf[:n]); writeErr != nil {
				outf.Close()
				f.Close()
				return nil, fmt.Errorf("failed to write to chunk: %w", writeErr)
			}
			written += int64(n)
		}

		if readErr == io.EOF {
			if outf != nil {
				outf.Close()
			}
			break
		}
		if readErr != nil {
			if outf != nil {
				outf.Close()
			}
			f.Close()
			return nil, fmt.Errorf("failed to read archive: %w", readErr)
		}

		if written >= maxBytes {
			outf.Close()
			outf = nil
			written = 0
		}
	}

	f.Close()
	if err := os.Remove(archivePath); err != nil {
		return nil, fmt.Errorf("failed to remove original archive after splitting: %w", err)
	}

	l.Infof("split archive [%s] into %d chunk(s)", filepath.Base(archivePath), len(chunks))
	return chunks, nil
}
