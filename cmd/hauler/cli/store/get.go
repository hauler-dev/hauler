package store

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/get"
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

	s.Open()
	defer s.Close()

	ref, err := name.ParseReference(reference)
	if err != nil {
		return err
	}

	eref := s.RelocateReference(ref)

	gopts := &get.Opts{
		DestinationDir: o.DestinationDir,
	}

	return get.Cmd(ctx, gopts, eref.Name())
}
