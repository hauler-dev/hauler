package serve

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/version"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/internal/server"
)

type RegistryOpts struct {
	Root       string
	Port       int
	ConfigFile string
}

func (o *RegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Root, "root", "r", ".", "Path to root of the directory to serve")
	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to a config file, will override all other configs")
}

func RegistryCmd(ctx context.Context, o *RegistryOpts) error {
	ctx = dcontext.WithVersion(ctx, version.Version)

	cfg := o.defaultConfig()
	if o.ConfigFile != "" {
		ucfg, err := loadConfig(o.ConfigFile)
		if err != nil {
			return err
		}
		cfg = ucfg
	}

	s, err := server.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}

	// TODO: Graceful cancelling

	return nil
}

func loadConfig(filename string) (*configuration.Configuration, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return configuration.Parse(f)
}

func (o *RegistryOpts) defaultConfig() *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": o.Root},

			// TODO: Ensure this is toggleable via cli arg if necessary
			// "maintenance": configuration.Parameters{"readonly.enabled": false},
		},
	}
	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	return cfg
}
