package get

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type ImageOpts struct{}

func (o *ImageOpts) AddFlags(cmd *cobra.Command) {}

func ImageCmd(ctx context.Context, o *ImageOpts) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler get image`")

	return nil
}
