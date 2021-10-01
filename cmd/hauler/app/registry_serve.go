package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	"github.com/spf13/cobra"
)

type imageServeOpts struct {
	*rootOpts
	*registryOpts

	// User defined
	port       int
	path       string
	configFile string

	// Generated
	registryCfg *configuration.Configuration
}

func NewRegistryServeCommand() *cobra.Command {
	opts := imageServeOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "serve the oci pull compliant registry from the local registry store",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.IntVarP(&opts.port, "port", "p", 5000, "Port to expose registry on")
	f.StringVar(&opts.path, "dir", "", "path to image store contents")
	f.StringVarP(&opts.configFile, "config", "c", "", "Path to registry config file, defaults will be created if left blank")

	return cmd
}

func (o *imageServeOpts) PreRun() error {
	if o.configFile == "" {
		o.logger.Infof("No config file set, using default values")
		o.registryCfg = o.defaultRegistryConfig()
	}

	return nil
}

func (o *imageServeOpts) Run() error {
	logger := o.rootOpts.logger

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r, err := registry.NewRegistry(ctx, o.registryCfg)
	if err != nil {
		return err
	}

	logger.Infof("Starting registry listening on :%d", o.port)
	if err = r.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func (o *imageServeOpts) defaultRegistryConfig() *configuration.Configuration {
	p := o.newStorePath(o.path)

	o.logger.Infof("Setting up registry using directory: %s", p.Path())
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": p.Path()},
		},
	}
	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	return cfg
}
