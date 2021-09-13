package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rancherfederal/hauler/pkg/bundler"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
	"github.com/spf13/cobra"
)

type bundleCreateOpts struct {
	bundleOpts

	name string
	root string
	images []string
	paths []string
}

func NewBundleCreateCommand() *cobra.Command {
	opts := &bundleCreateOpts{}

	cmd := &cobra.Command{
		Use: "create",
		Aliases: []string{"c"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.name, "name", "n", "", "Name of the bundle, used for file/directory naming")
	f.StringVarP(&opts.root, "root", "r", "", "Root directory of the bundle, defaults to current working directory")
	f.StringArrayVarP(&opts.images, "images", "i", []string{}, "Image to add to the bundle, can be specified multiple times")
	f.StringArrayVarP(&opts.paths, "paths", "p", []string{}, "Path to manifest(s) or chart(s) to add to the bundle, can be specified multiple times")

	return cmd
}

func (o *bundleCreateOpts) PreRun() error {
	if o.root == "" {
		if wd, err := os.Getwd(); err != nil {
			return err
		} else {
			o.root = wd
		}
	}

	return nil
}

func (o *bundleCreateOpts) Run() error {
	logger := log.NewLogger(os.Stdout)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := store.NewOciLayout(filepath.Join(o.root, o.name))
	if err != nil {
		return err
	}

	cfg := bundler.BundleConfig{
		Images: o.images,
		Paths: o.paths,
		Path: o.root,
	}

	b, err := bundler.NewBundle(ctx, store, cfg, logger)
	if err != nil {
		return err
	}

	return b.Bundle(ctx)
}
