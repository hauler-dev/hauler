package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	"github.com/distribution/distribution/v3/registry/handlers"
	"github.com/docker/go-metrics"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewRegistry(ctx context.Context, cfg *configuration.Configuration) (*registry.Registry, error) {
	r, err := registry.NewRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if cfg.HTTP.Debug.Prometheus.Enabled {
		path := cfg.HTTP.Debug.Prometheus.Path
		if path == "" {
			path = "/metrics"
		}
		http.Handle(path, metrics.Handler())
	}

	return r, nil
}

type tmpRegistryServer struct {
	*httptest.Server
}

func NewTempRegistry(ctx context.Context, root string) *tmpRegistryServer {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": root},
		},
	}
	// Add validation configuration
	cfg.Validation.Manifests.URLs.Allow = []string{".+"}
	
	cfg.Log.Level = "error"
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	l, err := logrus.ParseLevel("panic")
	if err != nil {
		l = logrus.ErrorLevel
	}
	logrus.SetLevel(l)

	app := handlers.NewApp(ctx, cfg)
	app.RegisterHealthChecks()
	handler := alive("/", app)

	s := httptest.NewUnstartedServer(handler)
	return &tmpRegistryServer{
		Server: s,
	}
}

// Registry returns the URL of the server without the protocol, suitable for content references
func (t *tmpRegistryServer) Registry() string {
	return strings.Replace(t.Server.URL, "http://", "", 1)
}

func (t *tmpRegistryServer) Start() error {
	t.Server.Start()

	err := retry(5, 1*time.Second, func() (err error) {
		resp, err := http.Get(t.Server.URL + "/v2")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		return errors.New("to start temporary registry")

	})
	return err
}

func (t *tmpRegistryServer) Stop() {
	t.Server.Close()
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

func retry(attempts int, sleep time.Duration, f func() error) (err error) {
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(sleep)
			sleep *= 2
		}
		err = f()
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("after %d attempts, last error: %s", attempts, err)
}
