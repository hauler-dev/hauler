package server

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

func NewFile(ctx context.Context, root string) (Server, error) {
	http.FileServer(http.Dir(root))

	r := mux.NewRouter()
	r.Handle("/", handlers.LoggingHandler(os.Stdout, http.FileServer(http.Dir(root))))

	srv := &http.Server{
		Handler:      r,
		Addr:         ":8000",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	return srv, nil
}
