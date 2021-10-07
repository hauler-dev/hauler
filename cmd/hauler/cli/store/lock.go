package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/rancherfederal/hauler/pkg/log"
)

type LockOpts struct {
	StoreFiles string
}

func (o *LockOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.StoreFiles, "files", "f", "", "Path to store files")
}

func LockCmd(ctx context.Context, o *LockOpts) error {
	l := log.FromContext(ctx)
	l.Debugf("running cli command `hauler store lock`")

	cs, err := contentStoreFromConfigFile(ctx, o.StoreFiles)
	if err != nil {
		return err
	}

	for _, f := range cs.Spec.Files {
		l.Infof("Locking file: %s", f.Path)
	}

	for _, h := range cs.Spec.Https {
		l.Infof("Locking https: %s", h.Url)
		if err := h.Lock(); err != nil {
			return err
		}
	}

	for _, r := range cs.Spec.Repos {
		l.Infof("Locking git repository: %s", r.Repo)
		if err := r.Lock(); err != nil {
			return err
		}
	}

	for _, i := range cs.Spec.Images {
		l.Infof("Locking image: %s", i.Reference)
		if err := i.Lock(); err != nil {
			return err
		}
	}

	for _, c := range cs.Spec.Charts {
		l.Infof("Locking chart: %s", c.Name)
		if err := c.Lock(); err != nil {
			return err
		}
	}

	lockFile := lockFileName(o.StoreFiles)

	data, err := yaml.Marshal(cs)
	if err != nil {
		return err
	}

	l.Infof("Writing lock file to %s", lockFile)
	if err = os.WriteFile(lockFile, data, os.ModePerm); err != nil {
		return err
	}

	return nil
}

// lockFileName will produce a lock filename from a filename
func lockFileName(filename string) string {
	dir := filepath.Dir(filename)
	ext := filepath.Ext(filename)
	base := strings.ReplaceAll(filepath.Base(filename), ext, "")

	return filepath.Join(dir, fmt.Sprintf("%s.lock%s", base, ext))
}
