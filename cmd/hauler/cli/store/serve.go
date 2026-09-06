package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/distribution/distribution/v3/configuration"
	_ "github.com/distribution/distribution/v3/registry/auth/htpasswd"
	_ "github.com/distribution/distribution/v3/registry/auth/silly"
	_ "github.com/distribution/distribution/v3/registry/auth/token"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/base"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/middleware/redirect"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/middleware/rewrite"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"gopkg.in/yaml.v3"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/internal/server"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
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

	if o.BasicAuth != "" {
		cfg.Auth = configuration.Auth{
			"htpasswd": configuration.Parameters{
				"realm": o.BasicAuthRealm,
				"path":  o.BasicAuth,
			},
		}
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
	} else if o.BasicAuth != "" {
		l.Infof("using basic auth via htpasswd file [%s] (realm [%s])", o.BasicAuth, o.BasicAuthRealm)
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

	f, err := server.NewFile(ctx, *o)
	if err != nil {
		return err
	}

	if o.BasicAuth != "" {
		l.Infof("using basic auth via htpasswd file [%s] (realm [%s])", o.BasicAuth, o.BasicAuthRealm)
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

func ServeGitCmd(ctx context.Context, o *flags.ServeGitOpts, s *store.Layout, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	if err := validateStoreExists(s); err != nil {
		return err
	}

	repos, err := extractGitRepos(ctx, s, o.RootDir)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return fmt.Errorf("no git repositories found in the store, add one with `hauler store add git <bundle>`")
	}
	l.Infof("found [%d] git repository(s) in the store", len(repos))

	g, err := server.NewGit(ctx, *o, repos)
	if err != nil {
		return err
	}

	if o.BasicAuth != "" {
		l.Infof("using basic auth via htpasswd file [%s] (realm [%s])", o.BasicAuth, o.BasicAuthRealm)
	}

	if o.TLSCert != "" && o.TLSKey != "" {
		l.Infof("starting git server with tls on port [%d]", o.Port)
		if err := g.ListenAndServeTLS(o.TLSCert, o.TLSKey); err != nil {
			return err
		}
	} else {
		l.Infof("starting git server on port [%d]", o.Port)
		if err := g.ListenAndServe(); err != nil {
			return err
		}
	}

	return nil
}

// extractGitRepos walks the store for artifacts tagged consts.GitRepoConfigMediaType (via `store add git`), extracting each into its own subdirectory of rootDir, and returns the name -> directory map NewGit needs to serve them.
func extractGitRepos(ctx context.Context, s *store.Layout, rootDir string) (map[string]string, error) {
	repos := map[string]string{}

	err := s.Walk(func(_ string, desc ocispec.Descriptor) error {
		// Walk yields synthetic composite keys too (e.g. "<ref>-<kind>" for cosign referrer bookkeeping), so skip anything without a proper ref name, same as CreateManifestCmd.
		refName, ok := desc.Annotations[ocispec.AnnotationRefName]
		if !ok {
			return nil
		}
		if kind := desc.Annotations[consts.KindAnnotationName]; kind != "" {
			if _, isCosignArtifact := consts.SigKindExt(kind); isCosignArtifact || strings.HasPrefix(kind, consts.KindAnnotationReferrers) {
				return nil
			}
		}

		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return nil
		}
		defer rc.Close()

		var m ocispec.Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil || m.Config.MediaType != consts.GitRepoConfigMediaType || len(m.Layers) == 0 {
			return nil
		}

		ref, err := reference.ParseReference(refName)
		if err != nil {
			return nil
		}
		name := strings.TrimPrefix(ref.Context().RepositoryStr(), consts.DefaultNamespace+"/")

		blobRC, err := s.Fetch(ctx, m.Layers[0])
		if err != nil {
			return fmt.Errorf("fetching git repository blob for [%s]: %w", name, err)
		}
		defer blobRC.Close()

		archivePath := filepath.Join(rootDir, name+".tar.gz")
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			return err
		}
		af, err := os.Create(archivePath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(af, blobRC); err != nil {
			af.Close()
			return fmt.Errorf("writing git repository archive for [%s]: %w", name, err)
		}
		af.Close()

		// Clear stale content before extracting so a repo re-served after an update (e.g. loose objects consolidated into a new pack by git gc on the source) never mixes old and new object state.
		repoDir := filepath.Join(rootDir, name)
		if err := os.RemoveAll(repoDir); err != nil {
			return err
		}
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return err
		}
		if err := server.ExtractRepo(archivePath, repoDir); err != nil {
			return fmt.Errorf("extracting git repository for [%s]: %w", name, err)
		}

		repos[name] = repoDir
		return nil
	})

	return repos, err
}
