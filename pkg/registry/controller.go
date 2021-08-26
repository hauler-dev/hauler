package registry

import (
	"net/http"

	"github.com/rancherfederal/hauler/pkg/store"
)

type Config struct {
	Path    string `json:"path"`
	Address string `json:"address"`
}

type Controller struct {
	Config *Config
	Server *http.Server
}

// NewController builds the registry controller
func NewController(config *Config) (*Controller, error) {
	oci, err := store.NewOci(config.Path)
	if err != nil {
		return nil, err
	}

	registry := NewRouteHandler(oci)

	server := &http.Server{
		Addr:    config.Address,
		Handler: registry.Setup(),
	}

	return &Controller{
		Config: config,
		Server: server,
	}, nil
}
