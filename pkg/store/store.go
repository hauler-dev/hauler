package store

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/registry/handlers"
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	"github.com/sirupsen/logrus"

	"github.com/rancherfederal/hauler/pkg/content"
)

const (
	// HaulerRepo is the repository path hauler uses to store oci artifacts
	HaulerRepo = "hauler"
)

var httpRegex = regexp.MustCompile("https?://")

// Store is a simple wrapper around distribution/distribution to enable hauler's use case
type Store struct {
	DataDir string

	config  *configuration.Configuration
	handler http.Handler

	server *httptest.Server
}

// Add will add an oci artifact to the registry store
func (r *Store) Add(ctx context.Context, ocis ...content.Oci) error {
	if err := r.precheck(); err != nil {
		return err
	}

	for _, o := range ocis {
		if err := o.Relocate(ctx, r.registryURL()); err != nil {
			return err
		}
	}

	return nil
}

// Remove will remove an oci artifact from the registry store
func (r *Store) Remove() error {
	if err := r.precheck(); err != nil {
		return err
	}

	return nil
}

// ListManifests will list all manifests in the registry store
func (r *Store) ListManifests() error {
	if err := r.precheck(); err != nil {
		return err
	}
	return nil
}

// ListReferences will list all references in the registry store
func (r *Store) ListReferences() error {
	if err := r.precheck(); err != nil {
		return err
	}
	return nil
}

// DefaultConfiguration does
func DefaultConfiguration(root string, addr string) *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": root},
		},
	}
	cfg.Log.Level = "panic"
	cfg.HTTP.Addr = addr
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}
	return cfg
}

// NewStore creates a new registry store, designed strictly for use within haulers embedded operations and _not_ for serving
func NewStore(ctx context.Context, dataDir string) *Store {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": dataDir},
		},
	}
	cfg.Log.Level = "panic"
	cfg.HTTP.Headers = http.Header{"X-Content-Type-Options": []string{"nosniff"}}

	handler := setupHandler(ctx, cfg)

	return &Store{
		DataDir: dataDir,

		config:  cfg,
		handler: handler,
	}
}

func NewRegistry(ctx context.Context, cfg *configuration.Configuration) (*Store, error) {
	ctx, _ = configureLogging(ctx, cfg)

	app := handlers.NewApp(ctx, cfg)
	app.RegisterHealthChecks()
	handler := alive("/", app)

	return &Store{
		config:  cfg,
		handler: handler,
	}, nil
}

// Start will create a new server and start it, it's up to the consumer to close it
func (r *Store) Start() *httptest.Server {
	server := httptest.NewServer(r.handler)
	r.server = server
	return server
}

func (r *Store) Stop() {
	r.server.Close()
	r.server = nil
	return
}

// precheck checks whether server is appropriately started and errors if it's not
// 		used to safely run Store operations without fear of panics
func (r *Store) precheck() error {
	if r.server == nil || r.server.URL == "" {
		return fmt.Errorf("server is not started yet")
	}
	return nil
}

// registryURL returns the registries URL without the protocol, suitable for image relocation operations
func (r *Store) registryURL() string {
	return httpRegex.ReplaceAllString(r.server.URL, "")
}

func alive(path string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// setupHandler will set up the registry handler
func setupHandler(ctx context.Context, config *configuration.Configuration) http.Handler {
	ctx, _ = configureLogging(ctx, config)

	app := handlers.NewApp(ctx, config)
	app.RegisterHealthChecks()
	handler := alive("/", app)

	return handler
}

func configureLogging(ctx context.Context, cfg *configuration.Configuration) (context.Context, context.CancelFunc) {
	logrus.SetLevel(logLevel(cfg.Log.Level))

	formatter := cfg.Log.Formatter
	if formatter == "" {
		formatter = "text"
	}

	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: time.RFC3339Nano,
	})

	if len(cfg.Log.Fields) > 0 {
		var fields []interface{}
		for k := range cfg.Log.Fields {
			fields = append(fields, k)
		}

		ctx = dcontext.WithValues(ctx, cfg.Log.Fields)
		ctx = dcontext.WithLogger(ctx, dcontext.GetLogger(ctx, fields...))
	}

	dcontext.SetDefaultLogger(dcontext.GetLogger(ctx))
	return context.WithCancel(ctx)
}

func logLevel(level configuration.Loglevel) logrus.Level {
	l, err := logrus.ParseLevel(string(level))
	if err != nil {
		l = logrus.InfoLevel
		logrus.Warnf("error parsing log level %q: %v, using %q", level, err, l)
	}
	return l
}
