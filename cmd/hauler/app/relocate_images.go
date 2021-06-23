package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pterm/pterm"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/oci"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
)

var (
	relocateImagesLong = `hauler relocate images processes a bundle provides by hauler package build and copies all of 
the collected images to a registry`

	relocateImagesExample = `
# Run Hauler
hauler relocate images locahost:5000 pkg.tar.zst 
`
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
		Args:    cobra.MinimumNArgs(2),
		Aliases: []string{"i", "img", "imgs"},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.destRef = args[0]
			opts.inputFile = args[1]
			return opts.Run(opts.destRef, opts.inputFile)
		},
	}

	return cmd
}

func (o *relocateImagesOpts) Run(dest string, input string) error {

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	o.logger.Debugf("Using temporary working directory: %s", tmpdir)

	a := packager.NewArchiver()

	if err := packager.Unpackage(a, input, tmpdir); err != nil {
		return err
	}
	o.logger.Debugf("Unpackaged %s", input)

	path := filepath.Join(tmpdir, v1alpha1.LayoutDir)

	ly, err := layout.FromPath(path)

	if err != nil {
		return err
	}

	// List images from OCI layout
	imgList := oci.ListImages(ly)

	// Create a pterm progressbar with style
	s := pterm.NewStyle(pterm.FgDefault)
	p, _ := pterm.DefaultProgressbar.WithTotal(len(imgList)).WithBarStyle(s).Start()

	// Loop through images in layout and copy to destination
	for imgName, imgHash := range imgList {

		n := strings.SplitN(imgName, "/", 2)

		img, err := ly.Image(imgHash)

		o.logger.Infof("Copying %s to %s", n[1], dest)
		p.Increment()

		if err != nil {
			return err
		}

		destName := dest + "/" + n[1]

		tag, err := name.ParseReference(destName)

		if err != nil {
			return err
		}

		if err := remote.Write(tag, img); err != nil {
			return err
		}

	}

	o.logger.Successf("Finished copying images to %s", dest)

	return nil
}
