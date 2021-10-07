package add

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type GenericOpts struct {
	Reference string
}

func (o *GenericOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Reference, "reference", "r", "", "Content reference")
}

func GenericCmd(ctx context.Context, o *GenericOpts, s *store.Store, refs ...string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler add generic`")

	l.With(log.Fields{"dir": s.DataDir}).Debugf("Opening store")
	s.Start()
	defer s.Stop()

	// generic, err := content.NewGeneric(o.Reference, refs[0])
	// if err != nil {
	// 	return err
	// }
	//
	// if err := s.Add(ctx, generic); err != nil {
	// 	return err
	// }
	//
	l.With(log.Fields{"dir": s.DataDir}).Debugf("Closing store")
	return nil
}
