package packager

import (
	"fmt"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/bundle"
	"os"
	"path/filepath"
)

//Export packages a bundle to a compressed tarball
func Export(b bundle.Bundle, bundleDir string, name string) error {
	z := newZstdArchiver()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	if err = os.Chdir(bundleDir); err != nil {
		return err
	}

	exportFileName := filepath.Join(cwd, fmt.Sprintf("%s.%s", name, z.String()))
	err = z.Archive([]string{"."}, exportFileName)
	if err != nil {
		return err
	}

	return nil
}

//Decompress will load a compressed archive bundle and unarchive it to dest
func Decompress(src string, dest string) error {
	z := newZstdArchiver()
	return z.Unarchive(src, dest)
}

func newZstdArchiver() archiver.TarZstd {
	return archiver.TarZstd{
		Tar: &archiver.Tar{
			OverwriteExisting:      true,
			MkdirAll:               true,
			ImplicitTopLevelFolder: false,
			StripComponents:        0,
			ContinueOnError:        false,
		},
	}
}