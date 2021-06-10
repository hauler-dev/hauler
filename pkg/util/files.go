package util

import (
	"bufio"
	"fmt"
	"github.com/mholt/archiver/v3"
	"io"
	"os"
	"path/filepath"
)

type dir struct {
	Path       string
	Permission os.FileMode
}

type FSLayout struct {
	Root string
	dirs []dir
}

type Layout interface {
	Create() error
	AddDir()
	Archive(archiver2 archiver.Archiver) error
	Remove() error
}

func NewLayout(root string) *FSLayout {
	absRoot, _ := filepath.Abs(root)
	return &FSLayout{
		Root: absRoot,
		dirs: nil,
	}
}

//Create will create the FSLayout at the FSLayout.Root
func (l FSLayout) Create() error {
	for _, dir := range l.dirs {
		fullPath := filepath.Join(l.Root, dir.Path)
		if err := os.MkdirAll(fullPath, dir.Permission); err != nil {
			return err
		}
	}
	return nil
}

//AddDir will add a folder to the FSLayout
func (l *FSLayout) AddDir(relPath string, perm os.FileMode) {
	l.dirs = append(l.dirs, dir{
		Path:       relPath,
		Permission: perm,
	})
}

func (l FSLayout) Remove() error {
	return os.RemoveAll(l.Root)
}

func (l FSLayout) Archive(a *archiver.TarZstd, name string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	err = os.Chdir(l.Root)

	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	archiveFile := filepath.Join(cwd, fmt.Sprintf("%s.%s", name, a.String()))
	if err := a.Archive([]string{"."}, archiveFile); err != nil {
		return err
	}

	return nil
}

func LinesToSlice(r io.ReadCloser) ([]string, error) {
	var lines []string

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
