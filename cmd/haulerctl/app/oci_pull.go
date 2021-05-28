package app

import (
	"context"
	"github.com/oras-project/oras-go/pkg/content"
	"github.com/oras-project/oras-go/pkg/oras"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type ociPullOpts struct {
	ociOpts

	sourceRef string
	outDir string
}

func NewOCIPullCommand() *cobra.Command {
	opts := ociPullOpts{}

	cmd := &cobra.Command{
		Use: "pull",
		Short: "oci pull",
		Aliases: []string{"p"},
		Args: cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.sourceRef = args[0]
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.outDir, "out-dir", "o", ".", "output directory")

	return cmd
}

func (o *ociPullOpts) PreRun() error {

	return nil
}

func (o *ociPullOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := content.NewFileStore(o.outDir)
	defer store.Close()

	allowedMediaTypes := []string{
		haulerMediaType,
	}

	resolver, err := o.resolver()
	if err != nil {
		return err
	}

	desc, _, err := oras.Pull(ctx, resolver, o.sourceRef, store, oras.WithAllowedMediaTypes(allowedMediaTypes))

	logrus.Infof("pulled %s with digest: %s", o.sourceRef, desc.Digest)

	return nil
}