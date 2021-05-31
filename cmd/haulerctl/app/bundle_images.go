package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type bundleImagesOpts struct {
	bundleOpts bundleOpts
}

// NewBundleImagesCommand creates a new sub command of bundle for images
func NewBundleImagesCommand() *cobra.Command {

	opts := &bundleImagesOpts{}

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Download a list of container images, new artifact containing all of them",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.bundleOpts.bundleDir = viper.GetString("bundledir")
			return opts.Run()
		},
	}

	return cmd
}

func (o *bundleImagesOpts) Run() error {

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

	fmt.Println("bundle images")
	fmt.Println(o.bundleOpts.bundleDir)

	return nil
}
