package bundler_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rancherfederal/hauler/pkg/bundler"
)

// MockOciStore mocks out image Add/Remove commands, these are tested elsewhere (such as in OciStore)
type MockOciStore struct{}

func (o MockOciStore) Add(ref name.Reference, opts ...remote.Option) error {
	return nil
}

func (o MockOciStore) Remove() error {
	return nil
}

// TODO: This test doesn't do anything
func TestNewBundle(t *testing.T) {
	ctx, _ := context.WithCancel(context.Background())
	defer ctx.Done()

	dir, err := os.MkdirTemp("", "hauler-bundler-test")
	if err != nil {
		t.Errorf("%s", err)
	}

	bCfg := bundler.BundleConfig{
		Images: []string{"alpine:latest"},
		Paths:  []string{"../../testdata/rawmanifests"},
		Path:   dir,
	}

	ms := MockOciStore{}

	b, err := bundler.NewBundle(ctx, ms, bCfg)
	if err != nil {
		t.Errorf("%s", err)
	}
}
