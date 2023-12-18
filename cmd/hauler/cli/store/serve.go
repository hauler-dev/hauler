package store

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/base"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	"github.com/distribution/distribution/v3/version"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/rancherfederal/hauler/internal/server"
)

type ServeOpts struct {
	*RootOpts

	Port       int
	RootDir    string
	ConfigFile string
	Daemon     bool

	storedir string
}

func (o *ServeOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on")
	f.StringVar(&o.RootDir, "directory", "registry", "Directory to use for registry backend (defaults to '$PWD/registry')")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to a config file, will override all other configs")
	f.BoolVarP(&o.Daemon, "daemon", "d", false, "Toggle serving as a daemon")
}

// ServeCmd serves the embedded registry almost identically to how distribution/v3 does it
func ServeCmd(ctx context.Context, o *ServeOpts, s *store.Layout) error {
	ctx = dcontext.WithVersion(ctx, version.Version)

	tr := server.NewTempRegistry(ctx, o.RootDir)
	if err := tr.Start(); err != nil {
		return err
	}

	opts := &CopyOpts{}
	if err := CopyCmd(ctx, opts, s, "registry://"+tr.Registry()); err != nil {
		return err
	}

	tr.Close()

	cfg := o.defaultConfig()
	if o.ConfigFile != "" {
		ucfg, err := loadConfig(o.ConfigFile)
		if err != nil {
			return err
		}
		cfg = ucfg
	}

	r, err := server.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	if err = r.ListenAndServe(); err != nil {
		return err
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

func (o *ServeOpts) defaultConfig() *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": o.RootDir},

			// TODO: Ensure this is toggleable via cli arg if necessary
			// "maintenance": configuration.Parameters{"readonly.enabled": false},
		},
	}

	// Add validation configuration
	cfg.Validation.Manifests.URLs.Allow = []string{".+"}

	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	return cfg
}
