package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/cache"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type rootOpts struct {
	logLevel string
	dataDir  string
	cacheDir string
}

var ro = &rootOpts{}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hauler",
		Short: "",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log.FromContext(cmd.Context()).SetLevel(ro.logLevel)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&ro.logLevel, "log-level", "l", "info", "")
	pf.StringVar(&ro.dataDir, "content-dir", "", "Location of where to create and store contents (defaults to ~/.local/hauler)")
	pf.StringVar(&ro.cacheDir, "cache", "", "Location of where to store cache data (defaults to $XDG_CACHE_DIR/hauler)")

	// Add subcommands
	addDownload(cmd)
	addStore(cmd)

	return cmd
}

func (o *rootOpts) getStore(ctx context.Context) (*store.Store, error) {
	dir := o.dataDir

	if o.dataDir == "" {
		// Default to userspace
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		abs, _ := filepath.Abs(filepath.Join(home, ".local/hauler/store"))
		if err := os.MkdirAll(abs, os.ModePerm); err != nil {
			return nil, err
		}

		dir = abs
	} else {
		// Make sure directory exists and we can write to it
		if f, err := os.Stat(o.dataDir); err != nil {
			return nil, err
		} else if !f.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", o.dataDir)
		} // TODO: Add writeable check

		abs, err := filepath.Abs(o.dataDir)
		if err != nil {
			return nil, err
		}

		dir = abs
	}

	s := store.NewStore(ctx, dir)
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
