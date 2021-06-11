package packager

import (
	"fmt"
	"github.com/mholt/archiver/v3"
	"os"
	"path/filepath"
)

type Archiver interface {
	String() string

	Archive([]string, string) error
	Unarchive(string, string) error
}

func NewArchiver() Archiver {
	return &archiver.TarZstd{
		Tar: &archiver.Tar{
			OverwriteExisting:      true,
			MkdirAll:               true,
			ImplicitTopLevelFolder: false,
			StripComponents:        0,
			ContinueOnError:        false,
		},
	}
}

func Package(a Archiver, src string, output string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	err = os.Chdir(src)
	if err != nil {
		return err
	}

	path := filepath.Join(cwd, fmt.Sprintf("%s.%s", output, a.String()))
	return a.Archive([]string{"."}, path)
}

func Unpackage(a Archiver, src, dest string) error {
	return a.Unarchive(src, dest)
}
