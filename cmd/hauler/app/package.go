package app

import (
	"context"

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

	cmd.AddCommand(NewPackageCreateCommand())
	cmd.AddCommand(NewPackageDeployCommand())

	f := cmd.Flags()
	_ = f

	return cmd
}

func (o *rootOpts) getStore(ctx context.Context, path string) (*store.Store, error) {
	o.logger.Debugf("Creating default store")

	p := o.newStorePath(path)

	o.logger.Debugf("Initializing content store at %s", p.Path())
	s := store.NewStore(ctx, p.Path())
	return s, nil
}
