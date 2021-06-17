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

type relocateImagesOpts struct {
	*rootOpts
	*relocateOpts
	destRef string
}

// NewRelocateImagesCommand creates a new sub command of relocate for images
func NewRelocateImagesCommand() *cobra.Command {
	opts := &relocateImagesOpts{
		rootOpts:     &ro,
		relocateOpts: &rlo,
	}

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Use artifact from bundle images to populate a target registry with the artifact's images",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.destRef = args[0]
			return opts.Run(opts.destRef)
		},
	}

	return cmd
}

func (o *relocateImagesOpts) Run(dst string) error {

	ar := packager.NewArchiver()

	tmpdir, err := os.MkdirTemp("", "hauler")

	if err != nil {
		o.logger.Errorf("error making temp directory: %v", err)
	}

	packager.Unpackage(ar, o.inputFile, tmpdir)

	if err != nil {
		o.logger.Errorf("error unpackaging bundle: %v", err)
	}

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
