package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type bundleArtifactsOpts struct {
	bundle *bundleOpts
}

// NewBundleArtifactsCommand creates a new sub command of bundle for artifacts
func NewBundleArtifactsCommand(bundle *bundleOpts) *cobra.Command {

	opts := &bundleArtifactsOpts{bundle: bundle}

	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Choose a folder on disk, new artifact containing all of folder's contents",
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	return cmd
}

func (o *bundleArtifactsOpts) Run() error {

	//TODO
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	//b := bundle.NewLayoutStore(o.bundleDir)
	//
	//images := []string{"alpine:latest", "registry:2.7.1"}
	//
	//for _, i := range images {
	//	if err := b.Add(ctx, i); err != nil {
	//		return err
	//	}
	//}
	_ = ctx

	fmt.Println("bundle artifacts")
	fmt.Println(o.bundle.bundleDir)

	return nil
}
