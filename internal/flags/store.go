package flags

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/hauler-dev/hauler/pkg/consts"
	"github.com/hauler-dev/hauler/pkg/log"
	"github.com/hauler-dev/hauler/pkg/store"
	"github.com/spf13/cobra"
)

type StoreRootOpts struct {
	StoreDir string
	CacheDir string
}

func (o *StoreRootOpts) AddFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVarP(&o.StoreDir, "store", "s", consts.DefaultStoreName, "Location to create store at")
	pf.StringVar(&o.CacheDir, "cache", "", "(deprecated flag and currently not used)")
}

func (o *StoreRootOpts) Store(ctx context.Context) (*store.Layout, error) {
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
