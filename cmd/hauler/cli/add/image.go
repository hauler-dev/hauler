package add

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type ImageOpts struct{}

func (o *ImageOpts) AddFlags(cmd *cobra.Command) {}

func ImageCmd(ctx context.Context, o *ImageOpts, s *store.Store, imageRefs ...string) error {
	l := log.FromContext(ctx)
	l.With(log.Fields{"dir": s.DataDir}).Debugf("running command `hauler add image`")

	l.With(log.Fields{"dir": s.DataDir}).Debugf("Opening store")
	s.Start()
	defer s.Stop()

	var imgs []content.Content
	for _, imageRef := range imageRefs {
		img := content.NewImage(imageRef)
		imgs = append(imgs, img)
	}

	// err := s.Add(ctx, imgs...)
	// if err != nil {
	// 	return err
	// }
	//
	l.With(log.Fields{"dir": s.DataDir}).Debugf("Closing store")
	return nil
}
