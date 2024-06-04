package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

type FileConfig struct {
	Root    string
	Host    string
	Port    int
	Timeout int
}

// NewFile returns a fileserver
// TODO: Better configs
func NewFile(ctx context.Context, cfg FileConfig) (Server, error) {
	r := mux.NewRouter()
	r.PathPrefix("/").Handler(handlers.LoggingHandler(os.Stdout, http.StripPrefix("/", http.FileServer(http.Dir(cfg.Root)))))
	if cfg.Root == "" {
		cfg.Root = "."
	}

	if cfg.Port == 0 {
		cfg.Port = 8080
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 60
	}

	srv := &http.Server{
		Handler:      r,
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		WriteTimeout: time.Duration(cfg.Timeout) * time.Second,
		ReadTimeout:  time.Duration(cfg.Timeout) * time.Second,
	}

	return srv, nil
}
