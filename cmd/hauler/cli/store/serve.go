package store

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type ServeOpts struct {
	Port       int
	ConfigFile string

	storedir string
}

func (o *ServeOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to a config file, will override all other configs")
}

// ServeCmd does
func ServeCmd(ctx context.Context, o *ServeOpts, s *store.Store) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler store serve`")

	cfg := o.defaultConfig(s)
	if o.ConfigFile != "" {
		ucfg, err := loadConfig(o.ConfigFile)
		if err != nil {
			return err
		}
		cfg = ucfg
	}

	r, err := registry.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	l.Infof("Starting registry listening on :%d", o.Port)
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

func (o *ServeOpts) defaultConfig(s *store.Store) *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": s.DataDir},

			// TODO: Ensure this is toggleable via cli arg if necessary
			"maintenance": configuration.Parameters{"readonly.enabled": true},
		},
	}
	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	return cfg
}
