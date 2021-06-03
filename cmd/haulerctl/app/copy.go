package app

import (
	"context"

	"github.com/oras-project/oras-go/pkg/content"
	"github.com/rancherfederal/hauler/pkg/copy"
	"github.com/spf13/cobra"
)

type copyOpts struct {
	dir                string
	allowedMediaTypes  []string
	allowAllMediaTypes bool
	sourceRef          string
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
			opts.sourceRef = args[0]
			return opts.Run(opts.sourceRef)
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&opts.allowedMediaTypes, "media-type", "t", nil, "allowed media types to be pulled")
	f.BoolVarP(&opts.allowAllMediaTypes, "allow-all", "a", false, "allow all media types to be pulled")
	f.StringVarP(&opts.dir, "dir", "d", ".", "Target directory for file copy")

	return cmd
}

// Run performs the operation.
func (o *copyOpts) Run(src string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	store := content.NewFileStore(o.dir)
	defer store.Close()

	if o.allowAllMediaTypes {
		o.allowedMediaTypes = nil
	} else if len(o.allowedMediaTypes) == 0 {
		o.allowedMediaTypes = []string{content.DefaultBlobMediaType, content.DefaultBlobDirMediaType}
	}

	cp := copy.NewCopier(o.dir, o.allowedMediaTypes, store)
	if err := cp.Get(ctx, src); err != nil {
		return err
	}

	return nil
}
