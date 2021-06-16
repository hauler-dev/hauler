package app

import (
	"context"

	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type copyOpts struct {
	dir       string
	sourceRef string
}

// NewCopyCommand creates a new sub command under
// hauler for coping files to local disk
func NewCopyCommand() *cobra.Command {
	opts := &copyOpts{}

	cmd := &cobra.Command{
		Use:     "copy",
		Short:   "Download artifacts from OCI registry to local disk",
		Aliases: []string{"c", "cp"},
		//Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.sourceRef = args[0]
			return opts.Run(opts.sourceRef)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.dir, "dir", "d", ".", "Target directory for file copy")

	return cmd
}

// Run performs the operation.
func (o *copyOpts) Run(src string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := oci.Get(ctx, src, o.dir); err != nil {
		logrus.Error(err)
	}

	return nil
}
