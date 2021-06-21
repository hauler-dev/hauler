package app

import (
	"context"

	ctxo "github.com/deislabs/oras/pkg/context"
	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/spf13/cobra"
)

type relocateArtifactsOpts struct {
	*relocateOpts
	destRef string
}

var (
	relocateArtifactsLong = `hauler relocate artifacts process an archive with files to be pushed to a registry`

	relocateArtifactsExample = `
# Run Hauler
hauler relocate artifacts locahost:5000/artifacts:latest artifacts.tar.zst 
`
)

// NewRelocateArtifactsCommand creates a new sub command of relocate for artifacts
func NewRelocateArtifactsCommand(relocate *relocateOpts) *cobra.Command {
	opts := &relocateArtifactsOpts{
		relocateOpts: relocate,
	}

	cmd := &cobra.Command{
		Use:     "artifacts",
		Short:   "Use artifact from bundle artifacts to populate a target file server with the artifact's contents",
		Long:    relocateArtifactsLong,
		Example: relocateArtifactsExample,
		Args:    cobra.MinimumNArgs(2),
		Aliases: []string{"a", "art", "af"},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.destRef = args[0]
			opts.inputFile = args[1]
			return opts.Run(opts.destRef, opts.inputFile)
		},
	}

	return cmd
}

func (o *relocateArtifactsOpts) Run(dst string, input string) error {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if loglevel != "debug" {
		ctx = ctxo.WithLoggerDiscarded(ctx)
	}

	if err := oci.Put(ctx, input, dst, o.logger); err != nil {
		o.logger.Errorf("error pushing artifact to registry %s: %v", dst, err)
	}

	return nil
}
