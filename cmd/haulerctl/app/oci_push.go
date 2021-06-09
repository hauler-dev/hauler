package app

import (
	"context"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/deislabs/oras/pkg/content"
	"github.com/deislabs/oras/pkg/oras"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"os"
)

type ociPushOpts struct {
	ociOpts

	targetRef string
	pathRef string
}

func NewOCIPushCommand() *cobra.Command {
	opts := ociPushOpts{}

	cmd := &cobra.Command{
		Use: "push",
		Short: "oci push",
		Aliases: []string{"p"},
		Args: cobra.MinimumNArgs(2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.pathRef = args[0]
			opts.targetRef = args[1]
			return opts.Run()
		},
	}

	return cmd
}

func (o *ociPushOpts) PreRun() error {

	return nil
}

func (o *ociPushOpts) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	data, err := os.ReadFile(o.pathRef)
	if err != nil {
		return err
	}

	resolver, err := o.resolver()
	if err != nil {
		return err
	}

	store := content.NewMemoryStore()

	contents := []ocispec.Descriptor{
		store.Add(o.pathRef, haulerMediaType, data),
	}

	desc, err := oras.Push(ctx, resolver, o.targetRef, store, contents)
	if err != nil {
		return err
	}

	logrus.Infof("pushed %s to %s with digest: %s", o.pathRef, o.targetRef, desc.Digest)

	return nil
}