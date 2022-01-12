package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/rancherfederal/ocil/pkg/layer"
	"github.com/rancherfederal/ocil/pkg/store"
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

	// TODO: Do we want this to be configurable?
	c, err := o.Cache(ctx)
	if err != nil {
		return nil, err
	}

	s, err := store.NewLayout(abs, store.WithCache(c))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (o *RootOpts) Cache(ctx context.Context) (layer.Cache, error) {
	dir := o.CacheDir

	if dir == "" {
		// Default to $XDG_CACHE_HOME
		cachedir, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}

		abs, _ := filepath.Abs(filepath.Join(cachedir, DefaultCacheDir))
		if err := os.MkdirAll(abs, os.ModePerm); err != nil {
			return nil, err
		}

		dir = abs
	}

	c := layer.NewFilesystemCache(dir)
	return c, nil
}
