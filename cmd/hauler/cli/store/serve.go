package store

import (
	"context"
	"os"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/base"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	"github.com/distribution/distribution/v3/version"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/internal/flags"
	"github.com/rancherfederal/hauler/internal/server"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type ServeRegistryOpts struct {
	*RootOpts

	Port       int
	RootDir    string
	ConfigFile string
	ReadOnly   bool
}

func (o *ServeRegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on.")
	f.StringVar(&o.RootDir, "directory", "registry", "Directory to use for backend.  Defaults to $PWD/registry")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to a config file, will override all other configs")
	f.BoolVar(&o.ReadOnly, "readonly", true, "Run the registry as readonly.")
}

func ServeRegistryCmd(ctx context.Context, o *flags.ServeRegistryOpts, s *store.Layout) error {
	l := log.FromContext(ctx)
	ctx = dcontext.WithVersion(ctx, version.Version)

	tr := server.NewTempRegistry(ctx, o.RootDir)
	if err := tr.Start(); err != nil {
		return err
	}

	opts := &flags.CopyOpts{}
	if err := CopyCmd(ctx, opts, s, "registry://"+tr.Registry()); err != nil {
		return err
	}

	tr.Close()

	cfg := o.DefaultRegistryConfig()
	if o.ConfigFile != "" {
		ucfg, err := loadConfig(o.ConfigFile)
		if err != nil {
			return err
		}
		cfg = ucfg
	}

	l.Infof("starting registry on port [%d]", o.Port)
	r, err := server.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	if err = r.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func ServeFilesCmd(ctx context.Context, o *flags.ServeFilesOpts, s *store.Layout) error {
	l := log.FromContext(ctx)
	ctx = dcontext.WithVersion(ctx, version.Version)

	opts := &flags.CopyOpts{}
	if err := CopyCmd(ctx, opts, s, "dir://"+o.RootDir); err != nil {
		return err
	}

	f, err := server.NewFile(ctx, *o)
	if err != nil {
		return err
	}

	if o.TLSCert != "" && o.TLSKey != "" {
		l.Infof("starting file server with tls on port [%d]", o.Port)
		if err := f.ListenAndServeTLS(o.TLSCert, o.TLSKey); err != nil {
			return err
		}
	} else {
		l.Infof("starting file server on port [%d]", o.Port)
		if err := f.ListenAndServe(); err != nil {
			return err
		}
	}

	return nil
}

func loadConfig(filename string) (*configuration.Configuration, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return configuration.Parse(f)
}
