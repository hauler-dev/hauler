package app

import (
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/spf13/cobra"
)

type ociOpts struct {
	insecure bool
	plainHTTP bool
}

const (
	haulerMediaType = "application/vnd.oci.image"
)

func NewOCICommand() *cobra.Command {
	opts := ociOpts{}

	cmd := &cobra.Command{
		Use: "oci",
		Short: "oci stuff",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewOCIPushCommand())
	cmd.AddCommand(NewOCIPullCommand())

	f := cmd.Flags()
	f.BoolVarP(&opts.insecure, "insecure", "", false, "Connect to registry without certs")
	f.BoolVarP(&opts.plainHTTP, "plain-http", "", false, "Connect to registry over plain http")

	return cmd
}

func (o *ociOpts) resolver() (remotes.Resolver, error) {
	resolver := docker.NewResolver(docker.ResolverOptions{PlainHTTP: true})
	return resolver, nil
}