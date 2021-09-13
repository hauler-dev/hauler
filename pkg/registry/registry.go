package registry

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/rancherfederal/hauler/pkg/store"
)

var (
	distributionContentDigestHeader = "Docker-Content-Digest"

	distributionAPIVersionHeader = "Docker-Distribution-Api-Version"
	distributionAPIVersion       = "registry/2.0"

	// https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pulling-manifests
	nameRegexp      = `[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*`
	referenceRegexp = `[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}`
)

type Registry struct {
	app    *App
	server *http.Server
}

type App struct {
	*fiber.App
}

func NewRegistry(cfg Config) (*Registry, error) {
	// var store store.DistributionStore
	var s *store.Layout
	var err error

	if len(cfg.Proxy.Remotes) > 0 {
		// TODO: set this up
	} else {
		// Default layout behavior
		s, err = store.NewLayout(cfg.Layout.Root)
		if err != nil {
			return nil, err
		}
	}

	app := fiber.New(fiber.Config{
		StreamRequestBody: true,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	app.Use(distributionHeadersMiddleware())

	// Routes
	router := NewV2Router(s)
	app.Mount("/", router.Routes())

	return &Registry{
		app: &App{
			App:   app,
		},
	}, nil
}

func (r *Registry) ListenAndServe() error {
	e := make(chan error)
	go func() {
		e <- r.app.Listen(":3333")
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-e:
		return err
	case <-c:
		_ = r.app.Shutdown()
		return nil
	}
}
