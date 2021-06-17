package app

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
)

type relocateArtifactsOpts struct {
	*rootOpts
	*relocateOpts
	destRef  string
}

// NewRelocateArtifactsCommand creates a new sub command of relocate for artifacts
func NewRelocateArtifactsCommand() *cobra.Command {
	opts := &relocateArtifactsOpts{
		rootOpts:     &ro,
		relocateOpts: &rlo,
	}

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
		o.logger.Errorf("error creating temporary directory hauler: %v", err)
	}

	packager.Unpackage(ar, o.inputFile, tmpdir)

	files, err := ioutil.ReadDir(tmpdir)

	if err != nil {
		o.logger.Errorf("error reading files from temporary directory: %v", err)
	}

	for _, f := range files {
		if err := oci.Put(ctx, filepath.Join(tmpdir, f.Name()), dst); err != nil {
			o.logger.Errorf("error pushing artifact to registry %s: %v", dst, err)
		}
	}

	return nil
}
