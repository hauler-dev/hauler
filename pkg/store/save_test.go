package store

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/pkg/artifacts/file"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// manifestEntry mirrors tarball.Descriptor for asserting manifest.json contents.
type manifestEntry struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

// readManifestJSON reads and unmarshals manifest.json from the given OCI layout dir.
func readManifestJSON(t *testing.T, dir string) []manifestEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, consts.ImageManifestFile))
	if err != nil {
		t.Fatalf("readManifestJSON: %v", err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("readManifestJSON unmarshal: %v", err)
	}
	return entries
}

// newTestStore creates a fresh store in a temp directory. Fatal on error.
func newTestStore(t *testing.T) *Layout {
	t.Helper()
	s, err := NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}
	return s
}

// newTestRegistry starts an in-memory OCI registry backed by httptest.
// Returns the host (host:port) and remote.Options that route requests through
// the server's plain-HTTP transport. The server is shut down via t.Cleanup.
func newTestRegistry(t *testing.T) (host string, remoteOpts []remote.Option) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts
}

// seedImage pushes a random single-platform image to the test registry.
// repo is a bare path like "myorg/myimage"; tag is the image tag string.
// Pass the remoteOpts from newTestRegistry so writes use the correct transport.
func seedImage(t *testing.T, host, repo, tag string, opts ...remote.Option) gcrv1.Image {
	t.Helper()
	img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("seedImage random.Image: %v", err)
	}
	ref, err := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatalf("seedImage name.NewTag: %v", err)
	}
	if err := remote.Write(ref, img, opts...); err != nil {
		t.Fatalf("seedImage remote.Write: %v", err)
	}
	return img
}

// seedIndex pushes a 2-platform image index (linux/amd64 + linux/arm64) to
// the test registry. Pass the remoteOpts from newTestRegistry.
func seedIndex(t *testing.T, host, repo, tag string, opts ...remote.Option) gcrv1.ImageIndex {
	t.Helper()
	amd64Img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("seedIndex random.Image amd64: %v", err)
	}
	arm64Img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("seedIndex random.Image arm64: %v", err)
	}
	idx := mutate.AppendManifests(
		empty.Index,
		mutate.IndexAddendum{
			Add: amd64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: arm64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)
	ref, err := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatalf("seedIndex name.NewTag: %v", err)
	}
	if err := remote.WriteIndex(ref, idx, opts...); err != nil {
		t.Fatalf("seedIndex remote.WriteIndex: %v", err)
	}
	return idx
}

// seedFileInHTTPServer starts an httptest server serving a single file at
// /filename with the given content. Returns the full URL. Server closed via t.Cleanup.
func seedFileInHTTPServer(t *testing.T, filename, content string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+filename, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		io.WriteString(w, content) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filename
}

// newTestContext returns a context with a no-op zerolog logger attached so that
// log.FromContext does not emit to stdout/stderr during tests.
func newTestContext(t *testing.T) context.Context {
	t.Helper()
	logger := zerolog.New(io.Discard)
	return logger.WithContext(context.Background())
}

// --------------------------------------------------------------------------
// WriteExportsManifest unit tests
// --------------------------------------------------------------------------

func TestWriteExportsManifest(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("no platform filter includes all platforms", func(t *testing.T) {
		host, rOpts := newTestRegistry(t)
		seedIndex(t, host, "test/multiarch", "v1", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/test/multiarch:v1", "", false, "", false, ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		if err := WriteExportsManifest(ctx, s.Root, ""); err != nil {
			t.Fatalf("WriteExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) < 2 {
			t.Errorf("expected >=2 entries (all platforms), got %d", len(entries))
		}
	})

	t.Run("linux/amd64 filter yields single entry", func(t *testing.T) {
		host, rOpts := newTestRegistry(t)
		seedIndex(t, host, "test/multiarch", "v2", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/test/multiarch:v2", "", false, "", false, ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		if err := WriteExportsManifest(ctx, s.Root, "linux/amd64"); err != nil {
			t.Fatalf("WriteExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) != 1 {
			t.Errorf("expected 1 entry for linux/amd64, got %d", len(entries))
		}
	})

}

func TestWriteExportsManifest_SkipsNonImages(t *testing.T) {
	ctx := newTestContext(t)

	url := seedFileInHTTPServer(t, "skip.sh", "#!/bin/sh\necho skip")
	s := newTestStore(t)
	f := file.NewFile(url)
	if _, err := s.AddArtifact(ctx, f, "skip.sh"); err != nil {
		t.Fatalf("AddArtifact (file): %v", err)
	}

	if err := WriteExportsManifest(ctx, s.Root, ""); err != nil {
		t.Fatalf("WriteExportsManifest: %v", err)
	}

	entries := readManifestJSON(t, s.Root)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for file-only store, got %d", len(entries))
	}
}

// TestWriteExportsManifest_DoesNotTouchOCILayoutMarker proves the function
// neither creates nor removes the oci-layout marker: the save/load dance
// around it is the caller's responsibility.
func TestWriteExportsManifest_DoesNotTouchOCILayoutMarker(t *testing.T) {
	ctx := newTestContext(t)

	host, rOpts := newTestRegistry(t)
	seedImage(t, host, "test/img", "v1", rOpts...)

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/img:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	markerPath := filepath.Join(s.Root, "oci-layout")
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: oci-layout should not exist in a fresh store, stat err=%v", err)
	}

	if err := WriteExportsManifest(ctx, s.Root, ""); err != nil {
		t.Fatalf("WriteExportsManifest: %v", err)
	}

	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Errorf("WriteExportsManifest created oci-layout (stat err=%v); it must only write manifest.json", err)
	}

	// Now plant a marker and prove it survives.
	if err := os.WriteFile(markerPath, []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("plant oci-layout: %v", err)
	}
	if err := WriteExportsManifest(ctx, s.Root, ""); err != nil {
		t.Fatalf("WriteExportsManifest (2nd): %v", err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("WriteExportsManifest removed the oci-layout marker: %v", err)
	}
}
