package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

type bundleArtifactsOpts struct {
	bundleOpts
}

func NewBundleArtifactsCommand() *cobra.Command {
	opts := &bundleArtifactsOpts{}

	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "bundle artifacts",
		Long:  "",
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

	fmt.Println("bundle images")

	return nil
}
