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
	cfg := server.FileConfig{
		Root: o.Root,
		Port: o.Port,
	}

	s, err := server.NewFile(ctx, cfg)
	if err != nil {
		return err
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}
	return nil
}
