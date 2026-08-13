package store_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"hauler.dev/go/hauler/v2/pkg/store"
)

// TestAddImage_ImageStatsAccumulation proves that attaching an *ImageStats
// to the context via WithImageStats before calling AddImage causes
// writeImageBlobs to record the image's layer count and total blob bytes,
// for cosmetic completion-line reporting in the CLI.
func TestAddImage_ImageStatsAccumulation(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	remoteOpts := []remote.Option{
		remote.WithTransport(srv.Client().Transport),
	}

	const numLayers = 3
	img, err := random.Image(512, numLayers)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}

	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("img.Layers: %v", err)
	}
	var wantBytes int64
	for _, l := range layers {
		size, err := l.Size()
		if err != nil {
			t.Fatalf("layer.Size: %v", err)
		}
		wantBytes += size
	}

	tag, err := gname.NewTag(host+"/test/image:v1", gname.Insecure)
	if err != nil {
		t.Fatalf("new tag: %v", err)
	}
	if err := remote.Write(tag, img, remoteOpts...); err != nil {
		t.Fatalf("push image: %v", err)
	}

	s, err := store.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("new layout: %v", err)
	}

	stats := &store.ImageStats{}
	ctx := store.WithImageStats(context.Background(), stats)

	if _, err := s.AddImage(ctx, tag.Name(), "", false, "", false, "", remoteOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	if got := stats.Layers.Load(); got != int64(numLayers) {
		t.Errorf("stats.Layers = %d, want %d", got, numLayers)
	}
	if got := stats.Bytes.Load(); got != wantBytes {
		t.Errorf("stats.Bytes = %d, want %d", got, wantBytes)
	}
}

// TestAddImage_NoImageStatsInContext proves AddImage works fine (no panic)
// when the context has no ImageStats attached -- the common case for every
// call site that doesn't care about stats.
func TestAddImage_NoImageStatsInContext(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	remoteOpts := []remote.Option{
		remote.WithTransport(srv.Client().Transport),
	}

	img, err := random.Image(512, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	tag, err := gname.NewTag(host+"/test/image:v1", gname.Insecure)
	if err != nil {
		t.Fatalf("new tag: %v", err)
	}
	if err := remote.Write(tag, img, remoteOpts...); err != nil {
		t.Fatalf("push image: %v", err)
	}

	s, err := store.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("new layout: %v", err)
	}

	if _, err := s.AddImage(context.Background(), tag.Name(), "", false, "", false, "", remoteOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
}
