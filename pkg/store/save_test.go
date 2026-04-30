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
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/pkg/artifacts/file"
	"hauler.dev/go/hauler/pkg/consts"
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
		if err := s.AddImage(ctx, host+"/test/multiarch:v1", "", false); err != nil {
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
		if err := s.AddImage(ctx, host+"/test/multiarch:v2", "", false); err != nil {
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

// --------------------------------------------------------------------------
// Exports type tests
// --------------------------------------------------------------------------

func TestExports_Digests(t *testing.T) {
	x := &Exports{
		digests: []string{"sha256:abc123", "sha256:def456"},
		records: map[string]tarball.Descriptor{},
	}

	d := x.Digests()
	if len(d) != 2 {
		t.Errorf("expected 2 digests, got %d", len(d))
	}
	if d[0] != "sha256:abc123" || d[1] != "sha256:def456" {
		t.Errorf("unexpected digests: %v", d)
	}
}

func TestExports_Records(t *testing.T) {
	x := &Exports{
		digests: []string{"sha256:abc123"},
		records: map[string]tarball.Descriptor{
			"sha256:abc123": {
				Config:   "blobs/sha256/abc",
				Layers:   []string{"blobs/sha256/layer1"},
				RepoTags: []string{"example.com/image:v1"},
			},
		},
	}

	r := x.Records()
	if len(r) != 1 {
		t.Errorf("expected 1 record, got %d", len(r))
	}
	if _, ok := r["sha256:abc123"]; !ok {
		t.Errorf("expected record for sha256:abc123")
	}
}

func TestExports_Records_ReturnsCopy(t *testing.T) {
	x := &Exports{
		digests: []string{"sha256:abc123"},
		records: map[string]tarball.Descriptor{
			"sha256:abc123": {
				Config:   "blobs/sha256/abc",
				Layers:   []string{"blobs/sha256/layer1"},
				RepoTags: []string{"example.com/image:v1"},
			},
		},
	}

	r := x.Records()
	// Modify the returned map - need to create a new descriptor since tarball.Descriptor
	// fields are immutable
	original := r["sha256:abc123"]
	r["sha256:abc123"] = tarball.Descriptor{
		Config:   original.Config,
		Layers:   append(original.Layers, "blobs/sha256/layer2"),
		RepoTags: append(original.RepoTags, "modified"),
	}
	r["sha256:newkey"] = tarball.Descriptor{
		Config:   "blobs/sha256/new",
		Layers:   []string{"blobs/sha256/layer1"},
		RepoTags: []string{"newtag"},
	}

	// Verify internal state is unchanged
	r2 := x.Records()
	if len(r2) != 1 {
		t.Errorf("expected 1 record after modification, got %d", len(r2))
	}
	if _, ok := r2["sha256:newkey"]; ok {
		t.Error("expected newkey to not be in internal records")
	}
	if len(r2["sha256:abc123"].RepoTags) != 1 {
		t.Errorf("expected 1 repo tag after modification, got %d", len(r2["sha256:abc123"].RepoTags))
	}
}

func TestExports_Records_NilWhenEmpty(t *testing.T) {
	x := &Exports{}
	r := x.Records()
	if r != nil {
		t.Errorf("expected nil for empty records, got %v", r)
	}
}
