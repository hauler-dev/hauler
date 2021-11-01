package store

import (
	"context"
	"fmt"
	"net/http"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type ServeOpts struct {
	Port       int
	configFile string
}

func (o *ServeOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on")
}

// ServeCmd does
func ServeCmd(ctx context.Context, o *ServeOpts, dir string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler store serve`")

	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": dir},

			// TODO: Ensure this is toggleable via cli arg if necessary
			"maintenance": configuration.Parameters{"readonly.enabled": true},
		},
	}
	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
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
