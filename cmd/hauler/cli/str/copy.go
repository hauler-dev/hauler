package str

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type CopyOpts struct{}

func (o *CopyOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	_ = f

	// TODO: Regex matching
}

func CopyCmd(ctx context.Context, o *CopyOpts, s *store.Store, registry string) error {
	lgr := log.FromContext(ctx)
	lgr.Debugf("running cli command `hauler store copy`")

	s.Open()
	defer s.Close()

	refs, err := s.List(ctx)
	if err != nil {
		return err
	}

	for _, r := range refs {
		ref, err := name.ParseReference(r, name.WithDefaultRegistry(s.Registry()))
		if err != nil {
			return err
		}

		o, err := remote.Image(ref)
		if err != nil {
			return err
		}

		rref, err := name.ParseReference(r, name.WithDefaultRegistry(registry))
		if err != nil {
			return err
		}

		lgr.Infof("relocating [%s] -> [%s]", ref.Name(), rref.Name())
		if err := remote.Write(rref, o); err != nil {
			return err
		}
	}

	return nil
}
