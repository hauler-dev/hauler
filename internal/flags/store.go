package flags

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

type StoreRootOpts struct {
	StoreDir          string
	Retries           int
	AllowHTTPRegistry bool
	AllowInsecure     bool
}

func (o *StoreRootOpts) AddFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.StringVarP(&o.StoreDir, "store", "s", "", "Set the directory to use for the content store")
	pf.IntVarP(&o.Retries, "retries", "r", consts.DefaultRetries, "Set the number of retries for operations")
	pf.BoolVarP(&o.AllowHTTPRegistry, "allow-http-registry", "", false, "Allow registry to use http endpoints")
	pf.BoolVarP(&o.AllowInsecure, "allow-insecure", "", false, "Allow insecure http endpoints")
}

func (o *StoreRootOpts) Store(ctx context.Context) (*store.Layout, error) {
	l := log.FromContext(ctx)

	storeDir := o.StoreDir

	if storeDir == "" {
		storeDir = os.Getenv(consts.HaulerStoreDir)
	}

	if storeDir == "" {
		storeDir = consts.DefaultStoreName
	}

	abs, err := filepath.Abs(storeDir)
	if err != nil {
		return nil, err
	}

	o.StoreDir = abs

	l.Debugf("using store at [%s]", abs)

	if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(abs, os.ModePerm); err != nil {
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
