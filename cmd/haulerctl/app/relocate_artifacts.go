package app

import (
	"context"
	"os"

	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type relocateArtifactsOpts struct {
	relocate *relocateOpts
	destRef  string
}

// NewRelocateArtifactsCommand creates a new sub command of relocate for artifacts
func NewRelocateArtifactsCommand(relocate *relocateOpts) *cobra.Command {
	opts := &relocateArtifactsOpts{relocate: relocate}

	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Use artifact from bundle artifacts to populate a target file server with the artifact's contents",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.destRef = args[0]
			return opts.Run(opts.destRef)
		},
	}

	return cmd
}

func (o *relocateArtifactsOpts) Run(dst string) error {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ar := packager.NewArchiver()

	tmpdir, err := os.MkdirTemp("", "hauler")

	if err != nil {
		logrus.Error(err)
	}

	packager.Unpackage(ar, o.relocate.bundleDir, tmpdir)

	if err := oci.Put(ctx, tmpdir, dst); err != nil {
		logrus.Error(err)
	}

	return nil
}
