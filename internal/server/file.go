package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// NewFile returns a fileserver
// TODO: Better configs
func NewFile(ctx context.Context, cfg flags.ServeFilesOpts) (Server, error) {
	if cfg.RootDir == "" {
		cfg.RootDir = "."
	}

	if cfg.Port == 0 {
		cfg.Port = consts.DefaultFileserverPort
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = consts.DefaultFileserverTimeout
	}

	if cfg.BasicAuthRealm == "" {
		cfg.BasicAuthRealm = consts.DefaultFileserverRealm
	}

	r := mux.NewRouter()
	r.Use(loggingMiddleware(log.FromContext(ctx)))

	if cfg.BasicAuth != "" {
		auth, err := loadHtpasswd(cfg.BasicAuth)
		if err != nil {
			return nil, err
		}
		r.Use(basicAuthMiddleware(auth, cfg.BasicAuthRealm))
	}

	r.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir(cfg.RootDir))))

	srv := &http.Server{
		Handler:      r,
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		WriteTimeout: time.Duration(cfg.Timeout) * time.Second,
		ReadTimeout:  time.Duration(cfg.Timeout) * time.Second,
	}

	return srv, nil
}
