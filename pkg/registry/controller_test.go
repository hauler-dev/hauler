package registry_test

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/rancherfederal/hauler/pkg/registry"
	"github.com/rancherfederal/hauler/pkg/store"
)

func TestRoutes(t *testing.T) {
	dir, err := ioutil.TempDir("", "hauler-registry-test")
	if err != nil {
		t.Errorf("Failed to create tmp directory for tests: %v", err)
	}

	defer os.RemoveAll(dir)

	o, err := store.NewOci(dir)
	if err != nil {
		t.Errorf("failed to create test oci directory: %v", err)
	}

	r := registry.NewRouteHandler(o)

	tcs := []struct {
		Description string
	}{
		{
			Description: "doit",
		},
	}

	// Spin up test server
	cfg := &registry.Config{
		Path: dir,
	}
	c, err := registry.NewController(cfg)
	if err != nil {
		t.Errorf("Failed to setup controller: %v", err)
	}

	_ = c
	_ = r

	for _, tc := range tcs {
		_ = tc
	}
}
