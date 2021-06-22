package app

import (
	"context"
	"io/ioutil"

	ctxo "github.com/deislabs/oras/pkg/context"
	"github.com/pterm/pterm"
	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/spf13/cobra"
)

var (
	copyLong = `hauler copies artifacts stored on a registry to local disk`

	copyExample = `
# Run Hauler
hauler copy locahost:5000/artifacts:latest
`
)

type copyOpts struct {
	*rootOpts
	dir       string
	sourceRef string
}

// NewCopyCommand creates a new sub command under
// hauler for coping files to local disk
func NewCopyCommand() *cobra.Command {
	opts := &copyOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:     "copy",
		Short:   "Download artifacts from OCI registry to local disk",
		Long:    copyLong,
		Example: copyExample,
		Aliases: []string{"c", "cp"},
		Args:    cobra.MinimumNArgs(1),
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

	target := o.dir

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// If loglevel is not set to debug, discard logging directly from ORAS library
	if loglevel != "debug" {
		ctx = ctxo.WithLoggerDiscarded(ctx)
	} else {
		// TODO: Route this to a log file or another way that doesn't clash with pterm
		ctx = ctxo.WithLoggerFromWriter(ctx, ioutil.Discard)
	}

	if o.dir == "." {
		target = "current directory"
	}

	// Create pterm spinner
	spinner, _ := pterm.DefaultSpinner.Start("Copying " + src + " to " + target)

	desc, err := oci.Get(ctx, src, o.dir, o.logger)

	if err != nil {
		o.logger.Errorf("error copy artifact %s to local directory %s: %v", src, o.dir, err)
	}

	// Finish spinner and send a success message
	spinner.Success("Pulled " + src + " to " + target + " with digest " + string(desc.Digest))

	return nil
}
