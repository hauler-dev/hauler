package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"hauler.dev/hauler/pkg/log"
	"hauler.dev/hauler/pkg/store"
)

const (
	DefaultStoreName = "store"
)

type RootOpts struct {
	StoreDir string
	CacheDir string
}

func (o *RootOpts) AddArgs(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVarP(&o.StoreDir, "store", "s", DefaultStoreName, "Location to create store at")
	pf.StringVar(&o.CacheDir, "cache", "", "(deprecated flag and currently not used)")
}

func (o *RootOpts) Store(ctx context.Context) (*store.Layout, error) {
	l := log.FromContext(ctx)
	dir := o.StoreDir

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	l.Debugf("using store at %s", abs)
	if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(abs, os.ModePerm)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	s, err := store.NewLayout(abs)
	if err != nil {
		return nil, err
	}
	return s, nil
}
