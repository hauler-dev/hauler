package serve

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/internal/server"
)

type FilesOpts struct {
	Root string
	Port int
}

func (o *FilesOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Root, "root", "r", ".", "Path to root of the directory to serve")
	f.IntVarP(&o.Port, "port", "p", 8080, "Port to listen on")
}

func FilesCmd(ctx context.Context, o *FilesOpts) error {
	s, err := server.NewFile(ctx, o.Root)
	if err != nil {
		return err
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}

	return nil
}
