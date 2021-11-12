package store

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/reference"
	"github.com/distribution/distribution/v3/registry/client"
	"github.com/distribution/distribution/v3/registry/handlers"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"

	// Init filesystem distribution storage driver
	_ "github.com/distribution/distribution/v3/registry/storage/driver/filesystem"

	"github.com/rancherfederal/hauler/pkg/cache"
)

var (
	httpRegex = regexp.MustCompile("https?://")
)

// Store is a simple wrapper around distribution/distribution to enable hauler's use case
type Store struct {
	DataDir           string
	DefaultRepository string

	config  *configuration.Configuration
	handler http.Handler
	server  *httptest.Server
	cache   cache.Cache
}

// NewStore creates a new registry store, designed strictly for use within haulers embedded operations and _not_ for serving
func NewStore(ctx context.Context, dataDir string, opts ...Options) *Store {
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

	s := &Store{
		DataDir: dataDir,
		config:  cfg,
		handler: handler,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Open will create a new server and start it, it's up to the consumer to close it
func (s *Store) Open() *httptest.Server {
	server := httptest.NewServer(s.handler)
	s.server = server
	return server
}

// Close stops the server
func (s *Store) Close() {
	s.server.Close()
	s.server = nil
	return
}

// List will list all known content tags in the registry
// TODO: This fn is messy and needs cleanup, this is arguably easier with the catalog api as well
func (s *Store) List(ctx context.Context) ([]string, error) {
	reg, err := client.NewRegistry(s.RegistryURL(), nil)
	if err != nil {
		return nil, err
	}

	entries := make(map[string]reference.Named)
	last := ""
	for {
		chunk := make([]string, 20) // randomly chosen number...
		nf, err := reg.Repositories(ctx, chunk, last)
		last = strconv.Itoa(nf)

		for _, e := range chunk {
			if e == "" {
				continue
			}

			ref, err := reference.WithName(e)
			if err != nil {
				return nil, err
			}
			entries[e] = ref
		}
		if err == io.EOF {
			break
		}
	}

	var refs []string
	for ref, named := range entries {
		repo, err := client.NewRepository(named, s.RegistryURL(), nil)
		if err != nil {
			return nil, err
		}

		tsvc := repo.Tags(ctx)
		ts, err := tsvc.All(ctx)
		if err != nil {
			continue
		}

		for _, t := range ts {
			ref, err := name.ParseReference(ref, name.WithDefaultRegistry(""), name.WithDefaultTag(t))
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref.Name())
		}
	}

	return refs, nil
}

// precheck checks whether server is appropriately started and errors if it's not
// 		used to safely run Store operations without fear of panics
func (s *Store) precheck() error {
	if s.server == nil || s.server.URL == "" {
		return fmt.Errorf("server is not started yet")
	}
	return nil
}

// Registry returns the registries URL without the protocol, suitable for image relocation operations
func (s *Store) Registry() string {
	return httpRegex.ReplaceAllString(s.server.URL, "")
}

// RegistryURL returns the registries URL
func (s *Store) RegistryURL() string {
	return s.server.URL
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
