package app

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/registry"
)

type imageServeOpts struct {
	port int
	path string
}

func NewRegistryServeCommand() *cobra.Command {
	opts := imageServeOpts{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "serve the oci pull copmliant registry from the local registry store",
		Run: func(cmd *cobra.Command, args []string) {
			opts.Run()
		},
	}

	f := cmd.Flags()
	f.IntVarP(&opts.port, "port", "p", 5000, "port to expose on")
	f.StringVarP(&opts.path, "store", "s", "hauler", "path to image store contents")

	return cmd
}

func (o imageServeOpts) Run() {
	cfg := registry.Config{
		Layout: registry.Layout{
			Root: o.path,
		},
		Proxy:  registry.Proxy{},
	}

	r, err := registry.NewRegistry(cfg)
	if err != nil {
		panic(err)
	}

	err = r.ListenAndServe()
	if err != nil {
		// TODO: don't panic
		panic(err)
	}
}
