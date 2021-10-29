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
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rancherfederal/hauler/pkg/content"
)

var (
	httpRegex = regexp.MustCompile("https?://")

	contents = make(map[metav1.TypeMeta]content.Oci)
)

// Store is a simple wrapper around distribution/distribution to enable hauler's use case
type Store struct {
	DataDir           string
	DefaultRepository string

	config  *configuration.Configuration
	handler http.Handler

	server *httptest.Server
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

		// TODO: Opt this
		DefaultRepository: "hauler",

		config:  cfg,
		handler: handler,
	}
}

// TODO: Refactor to a feature register model for content types
func Register(gvk metav1.TypeMeta, oci content.Oci) {
	if oci == nil {
		panic("store: Register content is nil")
	}
	if _, dup := contents[gvk]; dup {
		panic("store: Register called twice for content " + gvk.String())
	}
	contents[gvk] = oci
}

// Open will create a new server and start it, it's up to the consumer to close it
func (s *Store) Open() *httptest.Server {
	server := httptest.NewServer(s.handler)
	s.server = server
	return server
}

func (s *Store) Close() {
	s.server.Close()
	s.server = nil
	return
}

// Remove TODO: will remove an oci artifact from the registry store
func (s *Store) Remove() error {
	if err := s.precheck(); err != nil {
		return err
	}
	return nil
}

func RelocateReference(ref name.Reference, registry string) (name.Reference, error) {
	var sep string
	if _, err := name.NewDigest(ref.Name()); err == nil {
		sep = "@"
	} else {
		sep = ":"
	}
	return name.ParseReference(
		fmt.Sprintf("%s%s%s", ref.Context().RepositoryStr(), sep, ref.Identifier()),
		name.WithDefaultRegistry(registry),
	)
}

func (s *Store) RelocateReference(ref name.Reference) name.Reference {
	var sep string
	if _, err := name.NewDigest(ref.Name()); err == nil {
		sep = "@"
	} else {
		sep = ":"
	}
	relocatedRef, _ := name.ParseReference(
		fmt.Sprintf("%s%s%s", ref.Context().RepositoryStr(), sep, ref.Identifier()),
		name.WithDefaultRegistry(s.registryURL()),
	)
	return relocatedRef
}

// precheck checks whether server is appropriately started and errors if it's not
// 		used to safely run Store operations without fear of panics
func (s *Store) precheck() error {
	if s.server == nil || s.server.URL == "" {
		return fmt.Errorf("server is not started yet")
	}
	return nil
}

// registryURL returns the registries URL without the protocol, suitable for image relocation operations
func (s *Store) registryURL() string {
	return httpRegex.ReplaceAllString(s.server.URL, "")
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
