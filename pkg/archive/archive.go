package archive

import (
	"fmt"
	"github.com/mholt/archiver/v3"
	"os"
	"path/filepath"
)

type Archiver interface {
	// TODO: This isn't the greatest interface...
	Archive([]string, string) error
	String() string
}

type zstdArchiver archiver.TarZstd

func NewArchiver() *archiver.TarZstd {
	z := &archiver.TarZstd{
		Tar: &archiver.Tar{
			OverwriteExisting:      true,
			MkdirAll:               true,
			ImplicitTopLevelFolder: false,
			StripComponents:        0,
			ContinueOnError:        false,
		},
	}
	return z
}

func CompressAndArchive(a Archiver, source string, dest string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	err = os.Chdir(source)

	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	archivePath := filepath.Join(cwd, fmt.Sprintf("%s.%s", dest, a.String()))
	if err := a.Archive([]string{"."}, archivePath); err != nil {
		return err
	}

	return nil
}