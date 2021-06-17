package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
)

var (
	relocateImagesLong = `hauler relocate images processes a bundle provides by hauler
	package build and copies all of the collected images to a registry`

	relocateImagesExample = `
		# Run Hauler
		hauler relocate images pkg.tar.zst locahost:5000`
)

type relocateImagesOpts struct {
	*relocateOpts
	destRef string
}

// NewRelocateImagesCommand creates a new sub command of relocate for images
func NewRelocateImagesCommand(relocate *relocateOpts) *cobra.Command {
	opts := &relocateImagesOpts{
		relocateOpts: relocate,
	}

	cmd := &cobra.Command{
		Use:     "images",
		Short:   "Use artifact from bundle images to populate a target registry with the artifact's images",
		Long:    relocateImagesLong,
		Example: relocateImagesExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.inputFile = args[0]
			opts.destRef = args[1]
			return opts.Run(opts.destRef, opts.inputFile)
		},
	}

	return cmd
}

func (o *relocateImagesOpts) Run(dst string, input string) error {

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	o.logger.Debugf("Using temporary working directory: %s", tmpdir)

	a := packager.NewArchiver()

	if err := packager.Unpackage(a, input, tmpdir); err != nil {
		o.logger.Errorf("error unpackaging input %s: %v", input, err)
	}
	o.logger.Debugf("Unpackaged %s", input)

	path := filepath.Join(tmpdir, "layout")

	ly, err := layout.FromPath(path)

	if err != nil {
		o.logger.Errorf("error creating OCI layout: %v", err)
	}

	for nm, hash := range oci.ListImages(ly) {

		n := strings.SplitN(nm, "/", 2)

		img, err := ly.Image(hash)

		o.logger.Infof("Copy %s to %s", n[1], dst)

		if err != nil {
			o.logger.Errorf("error creating image from layout: %v", err)
		}

		dstimg := dst + "/" + n[1]

		tag, err := name.ParseReference(dstimg)

		if err != nil {
			o.logger.Errorf("err parsing destination image %s: %v", dstimg, err)
		}

		if err := remote.Write(tag, img); err != nil {
			o.logger.Errorf("error writing image to destination registry %s: %v", dst, err)
		}
	}

	return nil
}
