package bundle

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
)

type Bundle interface {
	//Sync
	Sync(context.Context, string) error
}

//Path represents a Bundle layout rooted in a filesystem
type Path string

func (p Path) Path(elem ...string) string {
	full := []string{string(p)}
	return filepath.Join(append(full, elem...)...)
}

//WriteFile is a helper function to write arbitrary data to path
func (p Path) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(p.Path(), os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}
	return ioutil.WriteFile(p.Path(name), data, perm)
}
