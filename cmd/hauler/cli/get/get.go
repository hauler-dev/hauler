package get

import (
	"context"
	"fmt"

	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Opts struct {
	DestinationDir string
}

func (o *Opts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
}

func Cmd(ctx context.Context, o *Opts, reference string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler get`")

	cs := content.NewFileStore(o.DestinationDir)
	defer cs.Close()

	ref, err := name.ParseReference(reference)
	if err != nil {
		return err
	}

	resolver := docker.NewResolver(docker.ResolverOptions{})

	desc, err := remote.Get(ref)
	if err != nil {
		return err
	}

	l.Debugf("Getting content of media type: %s", desc.MediaType)
	switch desc.MediaType {
	case ocispec.MediaTypeImageManifest:
		desc, artifacts, err := oras.Pull(ctx, resolver, ref.Name(), cs, oras.WithPullBaseHandler())
		if err != nil {
			return err
		}

		// TODO: Better logging
		_ = desc
		_ = artifacts
		// l.Infof("Downloaded %d artifacts: %s", len(artifacts), content.ResolveName(desc))

	case images.MediaTypeDockerSchema2Manifest:
		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		_ = img
	default:
		return fmt.Errorf("unknown media type: %s", desc.MediaType)
	}

	return nil
}
