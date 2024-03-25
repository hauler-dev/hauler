package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/rancherfederal/hauler/pkg/layer"
	"github.com/rancherfederal/hauler/pkg/store"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

const (
	DefaultStoreName = "store"
	DefaultCacheDir  = "hauler"
)

type RootOpts struct {
	StoreDir string
	CacheDir string
}

func (o *RootOpts) AddArgs(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVar(&o.CacheDir, "cache", "", "Location of where to store cache data (defaults to $XDG_CACHE_DIR/hauler)")
	pf.StringVarP(&o.StoreDir, "store", "s", DefaultStoreName, "Location to create store at")
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

	cd, err := o.getCacheDir()
	if err != nil {
		return nil, err
	}

	c := layer.NewFilesystemCache(cd)

	s, err := store.NewLayout(abs, store.WithCache(c, cd))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (o *RootOpts) getCacheDir() (string, error) {
	dir := o.CacheDir

	if dir == "" {
		// Default to $XDG_CACHE_HOME
		cachedir, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}

		abs, _ := filepath.Abs(filepath.Join(cachedir, DefaultCacheDir))
		if err := os.MkdirAll(abs, os.ModePerm); err != nil {
			return "", err
		}

		dir = abs
	}

	return dir, nil
}
