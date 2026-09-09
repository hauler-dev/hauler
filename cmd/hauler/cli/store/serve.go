package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/distribution/v3/configuration"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/base"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	"gopkg.in/yaml.v3"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/internal/repodata"
	"hauler.dev/go/hauler/v2/internal/server"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/store"
)

func validateStoreExists(s *store.Layout) error {
	indexPath := filepath.Join(s.Root, "index.json")

	_, err := os.Stat(indexPath)
	if err == nil {
		return nil
	}

	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf(
			"no store found at [%s]\n  ↳ does the hauler store exist? (verify with `hauler store info`)",
			s.Root,
		)
	}

	return fmt.Errorf(
		"unable to access store at [%s]: %w",
		s.Root,
		err,
	)
}

func loadConfig(filename string) (*configuration.Configuration, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return configuration.Parse(f)
}

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

	if err := validateStoreExists(s); err != nil {
		return err
	}

	tr := server.NewTempRegistry(ctx, o.RootDir)
	if err := tr.Start(); err != nil {
		return err
	}

	opts := &flags.CopyOpts{StoreRootOpts: rso, PlainHTTP: true}
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
		l.Infof("using registry configuration... \n%s", strings.TrimSpace(string(yamlConfig)))
	}

	l.Debugf("detailed registry configuration: %+v", cfg)

	r, err := server.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	if cfg.HTTP.Debug.Addr != "" {
		l.Infof("starting debug server on address [%s]", cfg.HTTP.Debug.Addr)
		if cfg.HTTP.Debug.Prometheus.Enabled {
			path := cfg.HTTP.Debug.Prometheus.Path
			if path == "" {
				path = "/metrics"
			}
			l.Infof("providing prometheus metrics on [%s]", path)
		}
	}
	server.ConfigureDebugServer(cfg)

	if err = r.ListenAndServe(); err != nil {
		return err
	}

	return nil
}

func ServeFilesCmd(ctx context.Context, o *flags.ServeFilesOpts, s *store.Layout, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	if err := validateStoreExists(s); err != nil {
		return err
	}

	opts := &flags.CopyOpts{StoreRootOpts: &flags.StoreRootOpts{}}
	if err := CopyCmd(ctx, opts, s, "directory://"+o.RootDir, ro); err != nil {
		return err
	}

	if o.GenerateRepodata {
		rpmCount, debCount, err := prepareRepodata(o.RootDir)
		if err != nil {
			return err
		}
		l.Infof("generated repodata for [%d] rpm and [%d] deb package(s)", rpmCount, debCount)
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

// prepareRepodata moves top-level .rpm/.deb files under rootDir into their own rpms/ and debs/ subdirectories and generates each package manager's repo metadata there.
func prepareRepodata(rootDir string) (rpmCount, debCount int, err error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return 0, 0, err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		lower := strings.ToLower(e.Name())
		switch {
		case strings.HasSuffix(lower, ".rpm"):
			if err := moveInto(rootDir, e.Name(), "rpms"); err != nil {
				return 0, 0, err
			}
			rpmCount++
		case strings.HasSuffix(lower, ".deb"):
			if err := moveInto(rootDir, e.Name(), "debs"); err != nil {
				return 0, 0, err
			}
			debCount++
		}
	}

	if rpmCount > 0 {
		if err := repodata.GenerateRPMRepo(filepath.Join(rootDir, "rpms")); err != nil {
			return 0, 0, fmt.Errorf("generating rpm repodata: %w", err)
		}
	}
	if debCount > 0 {
		if err := repodata.GenerateDebRepo(filepath.Join(rootDir, "debs")); err != nil {
			return 0, 0, fmt.Errorf("generating deb repodata: %w", err)
		}
	}

	// Report the repo's total package count, not just what moved this run, since a
	// restart with no new top-level files leaves earlier runs' packages in place.
	rpmTotal, err := countPackages(filepath.Join(rootDir, "rpms"), ".rpm")
	if err != nil {
		return 0, 0, err
	}
	debTotal, err := countPackages(filepath.Join(rootDir, "debs"), ".deb")
	if err != nil {
		return 0, 0, err
	}

	return rpmTotal, debTotal, nil
}

// countPackages counts files with ext directly under dir, or 0 if dir doesn't exist yet.
func countPackages(dir, ext string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ext) {
			count++
		}
	}
	return count, nil
}

// moveInto relocates rootDir/name into rootDir/subdir/name, creating subdir if needed.
func moveInto(rootDir, name, subdir string) error {
	destDir := filepath.Join(rootDir, subdir)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	return os.Rename(filepath.Join(rootDir, name), filepath.Join(destDir, name))
}
