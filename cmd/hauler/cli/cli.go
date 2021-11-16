package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/cache"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type rootOpts struct {
	logLevel string
	cacheDir string
	storeDir string
}

const defaultStoreLocation = "haul"

var ro = &rootOpts{}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hauler",
		Short: "",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			l := log.FromContext(cmd.Context())
			l.SetLevel(ro.logLevel)
			l.Debugf("running cli command [%s]", cmd.CommandPath())
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&ro.logLevel, "log-level", "l", "info", "")
	pf.StringVar(&ro.cacheDir, "cache", "", "Location of where to store cache data (defaults to $XDG_CACHE_DIR/hauler)")
	pf.StringVarP(&ro.storeDir, "store", "s", "", "Location to create store at (defaults to $PWD/store)")

	// Add subcommands
	addDownload(cmd)
	addStore(cmd)
	addVersion(cmd)

	return cmd
}

func (o *rootOpts) getStore(ctx context.Context) (*store.Store, error) {
	l := log.FromContext(ctx)
	dir := o.storeDir

	if dir == "" {
		l.Debugf("no store path specified, defaulting to $PWD/store")
		pwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}

		dir = filepath.Join(pwd, defaultStoreLocation)
	}

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
	c, err := o.getCache(ctx)
	if err != nil {
		return nil, err
	}

	s := store.NewStore(ctx, abs, store.WithCache(c))
	return s, nil
}

func (o *rootOpts) getCache(ctx context.Context) (cache.Cache, error) {
	dir := o.cacheDir

	if dir == "" {
		// Default to $XDG_CACHE_DIR
		cachedir, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}

		abs, _ := filepath.Abs(filepath.Join(cachedir, "hauler"))
		if err := os.MkdirAll(abs, os.ModePerm); err != nil {
			return nil, err
		}

		dir = abs
	}

	c := cache.NewFilesystem(dir)
	return c, nil
}
