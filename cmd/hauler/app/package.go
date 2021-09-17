package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/store"
)

const (
	defaultStoreLocation = ".local/hauler/store"
)

type packageOpts struct {
	*rootOpts
}

func NewPackageCommand() *cobra.Command {
	opts := packageOpts{
		&ro,
	}
	_ = opts

	cmd := &cobra.Command{
		Use:     "package",
		Short:   "package stuff",
		Aliases: []string{"p", "pack", "pkg"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewBundleCreateCommand())

	f := cmd.Flags()
	_ = f

	return cmd
}

func (o *rootOpts) getStore(ctx context.Context, path string) (*store.Store, error) {
	o.logger.Debugf("Creating default store")

	if path == "" {
		o.logger.Debugf("No path specified, using users default home directory as root")
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		path = filepath.Join(home, defaultStoreLocation)
	}

	o.logger.Debugf("Initializing content store at %s", path)
	s := store.NewStore(ctx, path)
	return s, nil
}
