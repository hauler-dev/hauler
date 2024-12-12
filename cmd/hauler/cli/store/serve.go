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
	"gopkg.in/yaml.v3"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/internal/server"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

func DefaultRegistryConfig(o *flags.ServeRegistryOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": o.RootDir},
			"maintenance": configuration.Parameters{
				"readonly": map[any]any{"enabled": o.ReadOnly},
			},
		},
	}

	if o.TLSCert != "" && o.TLSKey != "" {
		cfg.HTTP.TLS.Certificate = o.TLSCert
		cfg.HTTP.TLS.Key = o.TLSKey
	}

	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	cfg.Log.Level = configuration.Loglevel(ro.LogLevel)
	cfg.Validation.Manifests.URLs.Allow = []string{".+"}

	return cfg
}

func ServeRegistryCmd(ctx context.Context, o *flags.ServeRegistryOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	ctx = dcontext.WithVersion(ctx, version.Version)

	tr := server.NewTempRegistry(ctx, o.RootDir)
	if err := tr.Start(); err != nil {
		return err
	}

	opts := &flags.CopyOpts{}
	if err := CopyCmd(ctx, opts, s, "registry://"+tr.Registry(), ro); err != nil {
		return err
	}

	tr.Close()

	cfg := DefaultRegistryConfig(o, rso, ro)
	if o.ConfigFile != "" {
		ucfg, err := loadConfig(o.ConfigFile)
		if err != nil {
			return err
		}
		cfg = ucfg
	}

	l.Infof("starting registry on port [%d]", o.Port)

	yamlConfig, err := yaml.Marshal(cfg)
	if err != nil {
		l.Errorf("failed to validate/output registry configuration: %v", err)
	} else {
		l.Infof("using registry configuration...\n%s", string(yamlConfig))
	}

	l.Debugf("detailed registry configuration: %+v", cfg)

	r, err := server.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	if err = r.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func ServeFilesCmd(ctx context.Context, o *flags.ServeFilesOpts, s *store.Layout, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	ctx = dcontext.WithVersion(ctx, version.Version)

	opts := &flags.CopyOpts{}
	if err := CopyCmd(ctx, opts, s, "dir://"+o.RootDir, ro); err != nil {
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
