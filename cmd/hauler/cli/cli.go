package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/rancherfederal/ocil/pkg/layer"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/ocil/pkg/store"

	"github.com/rancherfederal/hauler/pkg/log"
)

type rootOpts struct {
	logLevel string
	cacheDir string
	storeDir string
}

var ro = &rootOpts{}

const (
	DefaultStoreName = "store"
	DefaultCacheDir  = "hauler"
)

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
	pf.StringVarP(&ro.storeDir, "store", "s", DefaultStoreName, "Location to create store at")

	// Add subcommands
	addDownload(cmd)
	addStore(cmd)
	addServe(cmd)
	addVersion(cmd)

	return cmd
}

func (o *rootOpts) getStore(ctx context.Context) (*store.Layout, error) {
	l := log.FromContext(ctx)
	dir := o.storeDir

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

	s, err := store.NewLayout(abs, store.WithCache(c))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (o *rootOpts) getCache(ctx context.Context) (layer.Cache, error) {
	dir := o.cacheDir

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
