package app

import (
	"fmt"
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
	relocate *relocateOpts
	destRef  string
}

// NewRelocateImagesCommand creates a new sub command of relocate for images
func NewRelocateImagesCommand(relocate *relocateOpts) *cobra.Command {
	opts := &relocateImagesOpts{relocate: relocate}

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
		return err
	}

	packager.Unpackage(ar, o.relocate.inputFile, tmpdir)

	if err != nil {
		return err
	}

	path := filepath.Join(tmpdir, "layout")

	ly, err := layout.FromPath(path)

	if err != nil {
		return err
	}

	for nm, hash := range oci.ListImages(ly) {

		n := strings.SplitN(nm, "/", 2)

		img, err := ly.Image(hash)

		fmt.Printf("Copy %s to %s", n[1], dst)
		fmt.Println()

		if err != nil {
			return err
		}

		dstimg := dst + "/" + n[1]

		tag, err := name.ParseReference(dstimg)

		if err != nil {
			return err
		}

		if err := remote.Write(tag, img); err != nil {
			return err
		}
	}

	return nil
}
