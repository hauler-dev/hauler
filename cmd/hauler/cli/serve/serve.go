package serve

import (
	"context"
	"fmt"
	"net/http"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/log"
)

type Opts struct {
	Port       int
	configFile string
}

func (o *Opts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on")
}

// Cmd does
func Cmd(ctx context.Context, o *Opts, dir string) error {
	l := log.FromContext(ctx)
	l.Debugf("running command `hauler serve`")

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
