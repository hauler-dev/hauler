package app

import (
	"context"

	"github.com/rancherfederal/hauler/pkg/copy"
	"github.com/spf13/cobra"
)

type copyOpts struct {
	dir       string
	mediatype string
	src       string
}

// NewCopyCommand creates a new sub command under
// haulerctl for coping files to local disk
func NewCopyCommand() *cobra.Command {
	opts := &copyOpts{}

	cmd := &cobra.Command{
		Use:     "copy",
		Short:   "Download artifacts from OCI registry to local disk",
		Aliases: []string{"c", "cp"},
		//Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.dir, "dir", "d", ".", "Target directory for file copy")
	f.StringVarP(&opts.src, "registry", "r", "localhost:5000/file:test", "URI for object in the registry")

	return cmd
}

// Run performs the operation.
func (o *copyOpts) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cp := copy.NewCopier(o.dir, o.mediatype)

	if err := cp.Get(ctx, o.src); err != nil {
		return err
	}

	return nil
}
