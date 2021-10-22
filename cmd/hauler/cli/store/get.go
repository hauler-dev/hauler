package store

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type GetOpts struct {
	DestinationDir string
}

func (o *GetOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.DestinationDir, "dir", "", "Directory to save contents to (defaults to current directory)")
}

func GetCmd(ctx context.Context, o *GetOpts, s *store.Store, reference string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler store get`")

	s.Start()
	defer s.Stop()

	cs := content.NewFileStore("")
	defer cs.Close()

	// resolver := docker.NewResolver(docker.ResolverOptions{})

	ref, err := name.ParseReference(reference)
	if err != nil {
		return err
	}

	eref := s.RelocateReference(ref)

	l.Infof("Getting %s", eref.Name())
	// desc, _, err := oras.Pull(ctx, resolver, eref.Name(), cs)
	// if err != nil {
	// 	return err
	// }
	//
	// l.Infof("Fetched '%s' of type '%s' with digest '%s'", eref.Name(), desc.MediaType, desc.Digest.String())

	i, err := remote.Image(eref)
	if err != nil {
		return err
	}

	if err := tarball.WriteToFile("wut.tar", eref, i); err != nil {
		return err
	}

	return nil
}
