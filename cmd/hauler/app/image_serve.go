package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/registry"
	"github.com/rancherfederal/hauler/pkg/store"
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

	r := registry.NewRouteHandler(ociLayout)

	server := &http.Server{
		Addr:        "0.0.0.0:3333",
		Handler:     r.Setup(),
		ReadTimeout: 5 * time.Second,
		IdleTimeout: 15 * time.Second,
	}

	serverCtx, serverStopCtx := context.WithCancel(context.Background())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-sig

		shutdownCtx, _ := context.WithTimeout(serverCtx, 30*time.Second)

		go func() {
			<-shutdownCtx.Done()
			if shutdownCtx.Err() == context.DeadlineExceeded {
				log.Fatal("graceful shutdown timed out... forcing exit")
			}
		}()

		err := server.Shutdown(shutdownCtx)
		if err != nil {
			log.Fatal(err)
		}
		serverStopCtx()
	}()

	// Run server
	fmt.Println("server up")
	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}

	<-serverCtx.Done()
}
