package archives

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mholt/archives"
	"hauler.dev/go/hauler/pkg/log"
)

// Maps to handle compression and archival types
var CompressionMap = map[string]archives.Compression{
	"gz":  archives.Gz{},
	"bz2": archives.Bz2{},
	"xz":  archives.Xz{},
	"zst": archives.Zstd{},
	"lz4": archives.Lz4{},
	"br":  archives.Brotli{},
}

var ArchivalMap = map[string]archives.Archival{
	"tar": archives.Tar{},
	"zip": archives.Zip{},
}

// check if a path exists
func isExist(path string) bool {
	_, statErr := os.Stat(path)
	return !os.IsNotExist(statErr)
}

// Archive is a function that archives the files in a directory
// dir: the directory to Archive
// outfile: the output file
// compression: the compression to use (gzip, bzip2, etc.)
// archival: the archival to use (tar, zip, etc.)
func Archive(ctx context.Context, dir, outfile string, compression archives.Compression, archival archives.Archival) error {
	l := log.FromContext(ctx)
	l.Debugf("Starting the archival process for directory: %s", dir)

	// remove outfile
	l.Debugf("Removing any existing output file: %s", outfile)
	if err := os.RemoveAll(outfile); err != nil {
		errMsg := fmt.Errorf("failed to remove existing output file '%s': %w", outfile, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}

	if !isExist(dir) {
		errMsg := fmt.Errorf("directory '%s' does not exist, cannot proceed with archival", dir)
		l.Debugf(errMsg.Error())
		return errMsg
	}

	// map files on disk to their paths in the archive
	l.Debugf("Mapping files in directory: %s", dir)
	archiveDirName := filepath.Base(filepath.Clean(dir))
	if dir == "." {
		archiveDirName = ""
	}
	files, err := archives.FilesFromDisk(context.Background(), nil, map[string]string{
		dir: archiveDirName,
	})
	if err != nil {
		errMsg := fmt.Errorf("error mapping files from directory '%s': %w", dir, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}
	l.Debugf("Successfully mapped files for directory: %s", dir)

	// create the output file we'll write to
	l.Debugf("Creating output file: %s", outfile)
	outf, err := os.Create(outfile)
	if err != nil {
		errMsg := fmt.Errorf("error creating output file '%s': %w", outfile, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}
	defer func() {
		l.Debugf("Closing output file: %s", outfile)
		outf.Close()
	}()

	// define the archive format
	l.Debugf("Defining the archive format with compression: %T and archival: %T", compression, archival)
	format := archives.CompressedArchive{
		Compression: compression,
		Archival:    archival,
	}

	// create the archive
	l.Debugf("Starting archive creation: %s", outfile)
	err = format.Archive(context.Background(), outf, files)
	if err != nil {
		errMsg := fmt.Errorf("error during archive creation for output file '%s': %w", outfile, err)
		l.Debugf(errMsg.Error())
		return errMsg
	}
	l.Debugf("Archive created successfully: %s", outfile)
	return nil
}
