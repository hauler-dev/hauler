package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type bundleArtifactsOpts struct {
	bundleOpts bundleOpts
}

// NewBundleArtifactsCommand creates a new sub command of bundle for artifacts
func NewBundleArtifactsCommand() *cobra.Command {

	opts := &bundleArtifactsOpts{}

	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Choose a folder on disk, new artifact containing all of folder's contents",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.bundleOpts.bundleDir = viper.GetString("bundledir")
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
	fmt.Println(o.bundleOpts.bundleDir)

	return nil
}
