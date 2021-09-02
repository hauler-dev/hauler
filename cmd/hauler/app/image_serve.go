package app

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/registry"
	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

type imageServeOpts struct {
	port int
	path string
}

func NewImageServeCommand() *cobra.Command {
	opts := imageServeOpts{}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "serve a read only oci pull compliant registry from the local image store",
		Run: func(cmd *cobra.Command, args []string) {
			opts.Run()
		},
	}

	f := cmd.Flags()
	f.IntVarP(&opts.port, "port", "p", 5000, "port to expose on")
	f.StringVarP(&opts.path, "store", "s", "hauler", "path to image store contents")

	return cmd
}

func (o imageServeOpts) Run() {
	ociLayout, err := store.NewOci(o.path)
	if err != nil {
		return
	}

	cfg := store.ProxyConfig{
		Registries: []store.Registry{
			{URL: "docker.io"},
			{URL: "ghcr.io"},
		},
	}
	p := store.NewProxy(cfg)

	// v2Registry := registry.NewRegistryV2Router(ociLayout)
	v2Registry := registry.NewRegistryV2Router(p)

	app := fiber.New(fiber.Config{
		IdleTimeout: 5 * time.Second,
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		// TODO: Better structured logging
		// Format: "{ ${pid}: ${status} }",
	}))

	app.Mount("/v2", v2Registry.Router())

	go func() {
		if err := app.Listen(":3333"); err != nil {
			log.Panic(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	_ = <-c
	fmt.Println("Gracefully shutting down...")
	_ = app.Shutdown()

	fmt.Println("Running cleanup tasks...")

	fmt.Println("Successfully shut down.")

	_ = ociLayout
	_ = p
	_ = app
	_ = v2Registry
}
