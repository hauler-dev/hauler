package store

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
	goname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"helm.sh/helm/v4/pkg/action"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/util"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// newLocalhostRegistry creates an in-memory OCI registry server listening on
// localhost (rather than 127.0.0.1) so go-containerregistry's Scheme() method
// automatically selects plain HTTP for "localhost:PORT/…" refs.  This is
// required for tests that exercise storeImage, which calls s.AddImage without
// any custom transport options.
func newLocalhostRegistry(t *testing.T) (host string, remoteOpts []remote.Option) {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("newLocalhostRegistry listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(registry.New())
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts
}

// chartTestdataDir is the relative path from cmd/hauler/cli/store/ to the
// top-level testdata directory, matching the convention in add_test.go.
// It must remain relative so that url.ParseRequestURI rejects it (an absolute
// path would be mistakenly treated as a URL by chart.NewChart's isUrl check).
const chartTestdataDir = "../../../../testdata"

// --------------------------------------------------------------------------
// Unit tests — unexported helpers
// --------------------------------------------------------------------------

func TestImagesFromChartAnnotations(t *testing.T) {
	tests := []struct {
		name    string
		chart   *helmchart.Chart
		want    []string
		wantErr bool
	}{
		{
			name:  "nil chart returns nil",
			chart: nil,
			want:  nil,
		},
		{
			name:  "no annotations returns nil",
			chart: &helmchart.Chart{Metadata: &helmchart.Metadata{}},
			want:  nil,
		},
		{
			name: "helm.sh/images annotation returns sorted refs",
			chart: &helmchart.Chart{
				Metadata: &helmchart.Metadata{
					Annotations: map[string]string{
						"helm.sh/images": "- image: nginx:1.24\n- image: alpine:3.18\n",
					},
				},
			},
			want: []string{"alpine:3.18", "nginx:1.24"},
		},
		{
			name: "both annotations with overlap returns deduped union",
			chart: &helmchart.Chart{
				Metadata: &helmchart.Metadata{
					Annotations: map[string]string{
						"helm.sh/images": "- image: nginx:1.24\n- image: alpine:3.18\n",
						"images":         "- image: nginx:1.24\n- image: busybox:latest\n",
					},
				},
			},
			want: []string{"alpine:3.18", "busybox:latest", "nginx:1.24"},
		},
		{
			name: "malformed YAML returns error",
			chart: &helmchart.Chart{
				Metadata: &helmchart.Metadata{
					Annotations: map[string]string{
						// Unclosed flow sequence → YAML syntax error.
						"helm.sh/images": "- image: [unclosed bracket",
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := imagesFromChartAnnotations(tc.chart)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImagesFromImagesLock(t *testing.T) {
	writeFile := func(dir, fname, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, fname), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", fname, err)
		}
	}

	t.Run("images.lock with image lines returns sorted refs", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "images.lock", "image: rancher/rancher:v2.9\nimage: nginx:1.24\n")
		got, err := imagesFromImagesLock(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"nginx:1.24", "rancher/rancher:v2.9"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("images-lock.yaml returns refs", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "images-lock.yaml", "image: alpine:3.18\n")
		got, err := imagesFromImagesLock(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"alpine:3.18"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("empty dir returns nil", func(t *testing.T) {
		dir := t.TempDir()
		got, err := imagesFromImagesLock(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("multiple lock files merged and deduped", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(dir, "images.lock", "image: nginx:1.24\nimage: alpine:3.18\n")
		writeFile(dir, "images-lock.yaml", "image: nginx:1.24\nimage: busybox:latest\n")
		got, err := imagesFromImagesLock(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"alpine:3.18", "busybox:latest", "nginx:1.24"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestApplyDefaultRegistry(t *testing.T) {
	tests := []struct {
		name     string
		img      string
		registry string
		want     string
		wantErr  bool
	}{
		{
			name:     "empty img returns empty",
			img:      "",
			registry: "myregistry.io",
			want:     "",
		},
		{
			name:     "empty registry returns img unchanged",
			img:      "rancher/rancher:v2.9",
			registry: "",
			want:     "rancher/rancher:v2.9",
		},
		{
			name:     "img without registry gets registry prepended",
			img:      "rancher/rancher:v2.9",
			registry: "myregistry.io",
			want:     "myregistry.io/rancher/rancher:v2.9",
		},
		{
			name:     "img with existing registry unchanged",
			img:      "ghcr.io/rancher/rancher:v2.9",
			registry: "myregistry.io",
			want:     "ghcr.io/rancher/rancher:v2.9",
		},
		{
			name:     "invalid ref with spaces returns error",
			img:      "invalid ref with spaces",
			registry: "myregistry.io",
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyDefaultRegistry(tc.img, tc.registry)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEncodeOriginalChartRef(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
		total   string
		want    string
	}{
		{
			name:    "repoURL and ref joined with pipe",
			repoURL: "https://charts.example.com",
			total:   "mychart:1.0.0",
			want:    "https://charts.example.com|mychart:1.0.0",
		},
		{
			name:    "empty repoURL still joined with pipe",
			repoURL: "",
			total:   "mychart:1.0.0",
			want:    "|mychart:1.0.0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeOriginalChartRef(tc.repoURL, tc.total)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRewriteReference(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("valid rewrite updates store annotations", func(t *testing.T) {
		host, rOpts := newTestRegistry(t)
		seedImage(t, host, "src/repo", "v1", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/src/repo:v1", "", false, "", false, "", rOpts...); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		oldRef, err := goname.NewTag(host+"/src/repo:v1", goname.Insecure)
		if err != nil {
			t.Fatalf("parse oldRef: %v", err)
		}
		newRef, err := goname.NewTag(host+"/dst/repo:v2", goname.Insecure)
		if err != nil {
			t.Fatalf("parse newRef: %v", err)
		}

		rawRewrite := newRef.String()

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}

		assertArtifactInStore(t, s, "dst/repo:v2")
	})

	t.Run("old ref not found returns error", func(t *testing.T) {
		s := newTestStore(t)
		oldRef, _ := goname.NewTag("docker.io/missing/repo:v1")
		newRef, _ := goname.NewTag("docker.io/new/repo:v2")
		rawRewrite := newRef.String()

		err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "could not find") {
			t.Errorf("expected 'could not find' in error, got: %v", err)
		}
	})

	// Tests for the registry-preservation / library/-stripping logic (lines 188-191).
	// go-containerregistry normalises bare single-name Docker Hub refs (e.g. "nginx:latest")
	// to "index.docker.io/library/nginx:latest". When the rewrite string omits a registry,
	// rewriteReference must (a) preserve the source registry and (b) strip the injected
	// "library/" prefix so that the stored ref looks like "nginx:v2", not "library/nginx:v2".

	t.Run("path-only rewrite strips library/ prefix from docker hub official image", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     "library/nginx:latest",
			consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
		})

		oldRef, _ := goname.NewTag("nginx:latest") // → index.docker.io/library/nginx:latest
		newRef, _ := goname.NewTag("nginx:v2")     // → index.docker.io/library/nginx:v2
		rawRewrite := "nginx:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// library/ must be stripped... registry stays index.docker.io
		assertAnnotationsInStore(t, s, "nginx:v2", "index.docker.io/nginx:v2")
	})

	t.Run("explicit docker.io rewrite preserves library/ prefix", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     "library/nginx:latest",
			consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
		})

		oldRef, _ := goname.NewTag("nginx:latest")
		newRef, _ := goname.NewTag("docker.io/nginx:v2") // → index.docker.io/library/nginx:v2
		rawRewrite := "docker.io/nginx:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// rawRewrite starts with "docker.io" → condition must NOT fire → library/ preserved
		assertAnnotationsInStore(t, s, "library/nginx:v2", "index.docker.io/library/nginx:v2")
	})

	t.Run("explicit index.docker.io rewrite preserves library/ prefix", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     "library/nginx:latest",
			consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
		})

		oldRef, _ := goname.NewTag("nginx:latest")
		newRef, _ := goname.NewTag("index.docker.io/nginx:v2") // → index.docker.io/library/nginx:v2
		rawRewrite := "index.docker.io/nginx:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// rawRewrite starts with "index.docker.io" → condition must NOT fire → library/ preserved
		assertAnnotationsInStore(t, s, "library/nginx:v2", "index.docker.io/library/nginx:v2")
	})

	t.Run("non-docker source with path-only rewrite preserves original registry", func(t *testing.T) {
		host, rOpts := newTestRegistry(t)
		seedImage(t, host, "src/repo", "v1", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/src/repo:v1", "", false, "", false, "", rOpts...); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		oldRef, _ := goname.NewTag(host+"/src/repo:v1", goname.Insecure)
		newRef, _ := goname.NewTag("newrepo/img:v2") // defaults to index.docker.io
		rawRewrite := "newrepo/img:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// condition fires → registry reverts to host, no library/ to strip
		assertAnnotationsInStore(t, s, "newrepo/img:v2", host+"/newrepo/img:v2")
	})

	// The library/-detection must look at rawRewrite's path (not the
	// go-containerregistry-normalized newRepo, which always carries "library/" for
	// single-segment repos), so that a rewrite which explicitly asks for
	// "library/..." is honored instead of being unconditionally stripped.

	t.Run("path-only rewrite with explicit library/ prefix is preserved", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     "library/nginx:latest",
			consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
		})

		oldRef, _ := goname.NewTag("nginx:latest")
		newRef, _ := goname.NewTag("library/nginx:v2")
		rawRewrite := "library/nginx:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// rewriteRepo (derived from rawRewrite) starts with "library/" → must be kept
		assertAnnotationsInStore(t, s, "library/nginx:v2", "index.docker.io/library/nginx:v2")
	})

	t.Run("leading slash rewrite with explicit library/ prefix is preserved", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     "library/nginx:latest",
			consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
		})

		oldRef, _ := goname.NewTag("nginx:latest")
		newRef, _ := goname.NewTag("library/nginx:v2")
		// AddImageCmd passes the pre-trim rewrite string through as rawRewrite, so a
		// leading "/" must still be handled correctly here.
		rawRewrite := "/library/nginx:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		assertAnnotationsInStore(t, s, "library/nginx:v2", "index.docker.io/library/nginx:v2")
	})
}

// TestRewriteReferenceMatchesContainerdNameVintage covers the #744 vintage gap:
// stores written by older hauler v2 carry "index.docker.io/..." in
// ContainerdImageNameKey, while stores written after the containerd-name
// normalization fix (Task 1/2) carry "docker.io/...". rewriteReference's match
// predicate compared that annotation verbatim, so it must be taught to match
// either vintage against a docker.io rewrite request.
func TestRewriteReferenceMatchesContainerdNameVintage(t *testing.T) {
	ctx := newTestContext(t)

	tests := []struct {
		name           string
		containerdName string
	}{
		{"legacy index.docker.io form", "index.docker.io/library/rewrite-legacy:v1"},
		{"normalized docker.io form", "docker.io/library/rewrite-legacy:v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			seedStoreDescriptor(t, s, map[string]string{
				ocispec.AnnotationRefName:     "library/rewrite-legacy:v1",
				consts.ContainerdImageNameKey: tc.containerdName,
			})

			oldRef, _ := reference.ParseReference("docker.io/library/rewrite-legacy:v1")
			newRef, _ := reference.ParseReference("docker.io/library/rewrite-new:v1")
			if err := rewriteReference(ctx, s, oldRef, newRef, "docker.io/library/rewrite-new:v1"); err != nil {
				t.Fatalf("rewriteReference should match %s: %v", tc.name, err)
			}
		})
	}
}

func TestRewriteChartReference(t *testing.T) {
	ctx := newTestContext(t)

	// A chart rewritten to a bare single-segment name must not keep an erroneous
	// "library/" prefix picked up from go-containerregistry's docker hub
	// normalization, unless the rewrite explicitly asked for one.

	t.Run("path-only rewrite strips library/ prefix from docker hub normalization", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName: "library/mychart:1.0.0",
		})

		ref, _ := goname.NewTag("mychart:1.0.0")
		if err := rewriteChartReference(ctx, s, ref, "mychart:2.0.0"); err != nil {
			t.Fatalf("rewriteChartReference: %v", err)
		}
		assertArtifactInStore(t, s, "mychart:2.0.0")
	})

	t.Run("explicit library/ prefix in rewrite is preserved", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName: "library/mychart:1.0.0",
		})

		ref, _ := goname.NewTag("mychart:1.0.0")
		if err := rewriteChartReference(ctx, s, ref, "library/mychart:2.0.0"); err != nil {
			t.Fatalf("rewriteChartReference: %v", err)
		}
		assertArtifactInStore(t, s, "library/mychart:2.0.0")
	})

	t.Run("leading slash rewrite with explicit library/ prefix is preserved", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName: "library/mychart:1.0.0",
		})

		ref, _ := goname.NewTag("mychart:1.0.0")
		if err := rewriteChartReference(ctx, s, ref, "/library/mychart:2.0.0"); err != nil {
			t.Fatalf("rewriteChartReference: %v", err)
		}
		assertArtifactInStore(t, s, "library/mychart:2.0.0")
	})

	t.Run("rewrite omitting tag inherits the source tag", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName: "library/mychart:1.0.0",
		})

		ref, _ := goname.NewTag("mychart:1.0.0")
		if err := rewriteChartReference(ctx, s, ref, "myneworg/mychart"); err != nil {
			t.Fatalf("rewriteChartReference: %v", err)
		}
		assertArtifactInStore(t, s, "myneworg/mychart:1.0.0")
	})
}

// --------------------------------------------------------------------------
// Integration tests
// --------------------------------------------------------------------------

func TestStoreFile(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("local file stored successfully", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "testfile-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		tmp.WriteString("hello hauler") //nolint:errcheck
		tmp.Close()

		s := newTestStore(t)
		if err := storeFile(ctx, s, v1.File{Path: tmp.Name()}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
			t.Fatalf("storeFile: %v", err)
		}
		assertArtifactInStore(t, s, filepath.Base(tmp.Name()))
		// OriginalRefAnnotation must capture the resolved absolute local path.
		assertOriginalRefInStore(t, s, filepath.Base(tmp.Name()), tmp.Name())
	})

	t.Run("HTTP URL stored under basename", func(t *testing.T) {
		url := seedFileInHTTPServer(t, "script.sh", "#!/bin/sh\necho ok")
		s := newTestStore(t)
		if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
			t.Fatalf("storeFile: %v", err)
		}
		assertArtifactInStore(t, s, "script.sh")
		// OriginalRefAnnotation must capture the original URL, not the local basename.
		assertOriginalRefInStore(t, s, "script.sh", url)
	})

	t.Run("name override changes stored ref", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "orig-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		tmp.Close()

		s := newTestStore(t)
		if err := storeFile(ctx, s, v1.File{Path: tmp.Name(), Name: "custom.sh"}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
			t.Fatalf("storeFile: %v", err)
		}
		assertArtifactInStore(t, s, "custom.sh")
	})

	t.Run("nonexistent local path returns error", func(t *testing.T) {
		s := newTestStore(t)
		err := storeFile(ctx, s, v1.File{Path: "/nonexistent/path/missing-file.txt"}, defaultCliOpts(), defaultRootOpts(s.Root))
		if err == nil {
			t.Fatal("expected error for nonexistent path, got nil")
		}
	})
}

func TestAddFileCmd(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	tmp, err := os.CreateTemp(t.TempDir(), "rawfile-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("raw content") //nolint:errcheck
	tmp.Close()

	// StoreRootOpts must be non-nil: storeFile now wraps its store write in
	// retry.Operation, which dereferences rso.Retries. In production this is
	// always set (see addStoreAddFile in cmd/hauler/cli/store.go); this test
	// previously left it nil since storeFile never touched rso before.
	o := &flags.AddFileOpts{Name: "renamed.txt", StoreRootOpts: defaultRootOpts(s.Root)}
	if err := AddFileCmd(ctx, o, s, tmp.Name(), defaultCliOpts()); err != nil {
		t.Fatalf("AddFileCmd: %v", err)
	}
	assertArtifactInStore(t, s, "renamed.txt")
}

func TestStoreImage(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "test/repo", "v1", rOpts...)

	tests := []struct {
		name         string
		imageName    string
		ignoreErrors bool
		wantErr      bool
		wantInStore  string
	}{
		{
			name:        "valid image tag stored",
			imageName:   host + "/test/repo:v1",
			wantInStore: "test/repo:v1",
		},
		{
			name:      "invalid ref string returns error",
			imageName: "INVALID IMAGE REF !! ##",
			wantErr:   true,
		},
		{
			name:         "nonexistent image with IgnoreErrors returns nil",
			imageName:    host + "/nonexistent/image:missing",
			ignoreErrors: true,
			wantErr:      false,
		},
		{
			name:      "nonexistent image without IgnoreErrors returns error",
			imageName: host + "/nonexistent/image:missing",
			wantErr:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()
			ro.IgnoreErrors = tc.ignoreErrors

			err := storeImage(ctx, s, v1.Image{Name: tc.imageName}, "", false, rso, ro, "", "", false)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantInStore != "" {
				assertArtifactInStore(t, s, tc.wantInStore)
			}
		})
	}

	t.Run("nonexistent image with HAULER_IGNORE_ERRORS env var returns nil and does not mutate ro", func(t *testing.T) {
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		t.Setenv(consts.HaulerIgnoreErrors, "true")

		err := storeImage(ctx, s, v1.Image{Name: host + "/nonexistent/image:missing"}, "", false, rso, ro, "", "", false)
		if err != nil {
			t.Fatalf("expected nil with HAULER_IGNORE_ERRORS=true, got: %v", err)
		}
		if ro.IgnoreErrors {
			t.Fatal("expected ro.IgnoreErrors to remain false: storeImage must not mutate ro")
		}
	})
}

// TestStoreImageIgnoreErrorsRelatedArtifactWording verifies that when the
// image itself lands in the store but a related-artifact probe fails (e.g. a
// 401 on the cosign tag convention), --ignore-errors reports that the image
// was stored -- not the generic "unable to add image ... skipping" wording,
// which would misreport that nothing was written.
func TestStoreImageIgnoreErrorsRelatedArtifactWording(t *testing.T) {
	host, opts := artifactProbe401Registry(t)
	seedImage(t, host, "repro/warn", "v1", opts...)

	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(context.Background())

	s := newTestStore(t)
	rso, ro := defaultRootOpts(s.Root), defaultCliOpts()
	ro.IgnoreErrors = true

	img := v1.Image{Name: host + "/repro/warn:v1"}
	if err := storeImage(ctx, s, img, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage with --ignore-errors returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "stored, but") {
		t.Fatalf("warning does not say the image was stored; got: %s", out)
	}
	if strings.Contains(out, "unable to add image") {
		t.Fatalf("misleading 'unable to add image' still logged for a related-artifact failure; got: %s", out)
	}

	// The image itself must be in the store.
	assertArtifactInStore(t, s, "repro/warn")
}

func TestStoreImage_Rewrite(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	t.Run("explicit rewrite tag changes ref", func(t *testing.T) {
		seedImage(t, host, "src/repo", "v1", rOpts...)
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		err := storeImage(ctx, s, v1.Image{Name: host + "/src/repo:v1"}, "", false, rso, ro, "newrepo/img:v2", "", false)
		if err != nil {
			t.Fatalf("storeImage with rewrite: %v", err)
		}
		assertArtifactInStore(t, s, "newrepo/img:v2")
		// OriginalRefAnnotation must retain the pre-rewrite source reference,
		// even though AnnotationRefName/ContainerdImageNameKey were rewritten.
		assertOriginalRefInStore(t, s, "newrepo/img:v2", host+"/src/repo:v1")
	})

	t.Run("rewrite without tag inherits source tag", func(t *testing.T) {
		seedImage(t, host, "src/repo", "v3", rOpts...)
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		err := storeImage(ctx, s, v1.Image{Name: host + "/src/repo:v3"}, "", false, rso, ro, "newrepo/img", "", false)
		if err != nil {
			t.Fatalf("storeImage with tagless rewrite: %v", err)
		}
		// tag is inherited from source ("v3")
		assertArtifactInStore(t, s, "newrepo/img:v3")
	})

	t.Run("rewrite without tag on digest source ref returns error", func(t *testing.T) {
		img := seedImage(t, host, "src/repo", "digest-src", rOpts...)
		h, err := img.Digest()
		if err != nil {
			t.Fatalf("img.Digest: %v", err)
		}

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		digestRef := host + "/src/repo@" + h.String()
		err = storeImage(ctx, s, v1.Image{Name: digestRef}, "", false, rso, ro, "newrepo/img", "", false)
		if err == nil {
			t.Fatal("expected error for digest ref rewrite without explicit tag, got nil")
		}
		if !strings.Contains(err.Error(), "cannot rewrite digest reference") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestStoreImage_TagAtDigest covers "repo:tag@sha256:..." refs: go-containerregistry
// parses these as a Digest and silently drops the tag from Name(), so storeImage
// must recover it via digestRefTag and record the tag-form annotation while still
// fetching the pinned digest.
func TestStoreImage_TagAtDigest(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	t.Run("repo:tag@digest stores under the tag, fetches the exact digest", func(t *testing.T) {
		img := seedImage(t, host, "tagdigest/repo", "v1", rOpts...)
		h, err := img.Digest()
		if err != nil {
			t.Fatalf("img.Digest: %v", err)
		}

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		ref := host + "/tagdigest/repo:v1@" + h.String()
		if err := storeImage(ctx, s, v1.Image{Name: ref}, "", false, rso, ro, "", "", false); err != nil {
			t.Fatalf("storeImage tag@digest: %v", err)
		}

		// The store must record the tag form, not the tag-dropped digest form
		// that r.Name() alone would have produced.
		assertArtifactInStore(t, s, "tagdigest/repo:v1")
		assertArtifactNotInStore(t, s, "tagdigest/repo@sha256")

		if got := storedDigest(t, s, "tagdigest/repo:v1"); got != h.String() {
			t.Errorf("stored digest = %q, want %q", got, h.String())
		}
	})

	t.Run("rewrite without explicit tag on a tag@digest source inherits the source tag", func(t *testing.T) {
		img := seedImage(t, host, "tagdigest/src", "v2", rOpts...)
		h, err := img.Digest()
		if err != nil {
			t.Fatalf("img.Digest: %v", err)
		}

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		ref := host + "/tagdigest/src:v2@" + h.String()
		if err := storeImage(ctx, s, v1.Image{Name: ref}, "", false, rso, ro, "newrepo/img", "", false); err != nil {
			t.Fatalf("storeImage with tagless rewrite on tag@digest source: %v", err)
		}
		// Tag is inherited from the source ("v2"), matching plain-tag rewrite
		// behavior -- this is only possible once the source tag is recovered.
		assertArtifactInStore(t, s, "newrepo/img:v2")
	})
}

func TestStoreImage_MultiArch(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedIndex(t, host, "test/multiarch", "v1", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/multiarch:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage multi-arch index: %v", err)
	}
	// Full index (both platforms) must be stored as an index, not a single image.
	assertArtifactKindInStore(t, s, "test/multiarch:v1", consts.KindAnnotationIndex)
}

func TestStoreImage_PlatformFilter(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedIndex(t, host, "test/multiarch", "v2", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/multiarch:v2"}, "linux/amd64", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage with platform filter: %v", err)
	}
	// Platform filter resolves a single manifest from the index → stored as a single image.
	assertArtifactKindInStore(t, s, "test/multiarch:v2", consts.KindAnnotationImage)
}

// A platform filter on a digest-pinned multi-arch index would store only the
// child under the index's digest, leaving the pinned digest unpullable after
// copy (#779). Both digest forms are rejected; --ignore-errors warns and skips.
func TestStoreImage_PlatformOnPinnedIndexRejected(t *testing.T) {
	host, rOpts := newLocalhostRegistry(t)
	idx := seedIndex(t, host, "pinned/multiarch", "v1", rOpts...)
	idxDigest, err := idx.Digest()
	if err != nil {
		t.Fatalf("idx.Digest: %v", err)
	}

	cases := []struct{ name, ref string }{
		{name: "plain repo@digest", ref: host + "/pinned/multiarch@" + idxDigest.String()},
		{name: "repo:tag@digest", ref: host + "/pinned/multiarch:v1@" + idxDigest.String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("fails and stores nothing", func(t *testing.T) {
				ctx := newTestContext(t)
				s := newTestStore(t)
				err := storeImage(ctx, s, v1.Image{Name: tc.ref}, "linux/amd64", false, defaultRootOpts(s.Root), defaultCliOpts(), "", "", false)
				if err == nil {
					t.Fatal("storeImage accepted a platform on a digest-pinned index")
				}
				if !strings.Contains(err.Error(), "multi-platform index") {
					t.Fatalf("error does not explain the rejection: %v", err)
				}
				if got := countArtifactsInStore(t, s); got != 0 {
					t.Fatalf("store holds %d artifacts, want 0", got)
				}
			})

			t.Run("--ignore-errors warns and skips", func(t *testing.T) {
				var buf bytes.Buffer
				ctx := log.NewLogger(&buf).WithContext(context.Background())
				s := newTestStore(t)
				ro := defaultCliOpts()
				ro.IgnoreErrors = true
				if err := storeImage(ctx, s, v1.Image{Name: tc.ref}, "linux/amd64", false, defaultRootOpts(s.Root), ro, "", "", false); err != nil {
					t.Fatalf("storeImage with --ignore-errors returned error: %v", err)
				}
				out := buf.String()
				if !strings.Contains(out, "multi-platform index") || !strings.Contains(out, "skipping") {
					t.Fatalf("expected a WARN naming the skipped image; got: %s", out)
				}
				if got := countArtifactsInStore(t, s); got != 0 {
					t.Fatalf("store holds %d artifacts, want 0", got)
				}
			})
		})
	}
}

// A digest that already names a single-platform child is stored as before:
// the user pinned exactly the bytes a platform pull would select.
func TestStoreImage_PlatformOnPinnedChildPassesThrough(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	idx := seedIndex(t, host, "pinned/child", "v1", rOpts...)
	im, err := idx.IndexManifest()
	if err != nil {
		t.Fatalf("IndexManifest: %v", err)
	}
	var childDigest gcrv1.Hash
	for _, m := range im.Manifests {
		if m.Platform != nil && m.Platform.Architecture == "amd64" {
			childDigest = m.Digest
		}
	}
	if childDigest.String() == "" {
		t.Fatal("seedIndex produced no linux/amd64 child")
	}

	s := newTestStore(t)
	ref := host + "/pinned/child@" + childDigest.String()
	if err := storeImage(ctx, s, v1.Image{Name: ref}, "linux/amd64", false, defaultRootOpts(s.Root), defaultCliOpts(), "", "", false); err != nil {
		t.Fatalf("storeImage on a pinned single-platform child: %v", err)
	}
	assertArtifactKindInStore(t, s, "pinned/child@", consts.KindAnnotationImage)
	if got := storedDigest(t, s, "pinned/child@"); got != childDigest.String() {
		t.Errorf("stored digest = %q, want %q", got, childDigest.String())
	}
}

func TestStoreImage_CosignV2Artifacts(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img := seedImage(t, host, "test/signed", "v1", rOpts...)
	seedCosignV2Artifacts(t, host, "test/signed", img, rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/signed:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}
	assertArtifactKindInStore(t, s, "test/signed:v1", consts.KindAnnotationSigs)
	assertArtifactKindInStore(t, s, "test/signed:v1", consts.KindAnnotationAtts)
	assertArtifactKindInStore(t, s, "test/signed:v1", consts.KindAnnotationSboms)
}

func TestStoreImage_CosignV3Referrer(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img := seedImage(t, host, "test/image", "v1", rOpts...)
	seedOCI11Referrer(t, host, "test/image", img, rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/image:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}
	assertReferrerInStore(t, s, "test/image:v1")
}

func TestStoreImage_ExcludeExtras(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("cosign v2 artifacts excluded when excludeExtras=true", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)

		img := seedImage(t, host, "test/signed", "v1", rOpts...)
		seedCosignV2Artifacts(t, host, "test/signed", img, rOpts...)

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		if err := storeImage(ctx, s, v1.Image{Name: host + "/test/signed:v1"}, "", true, rso, ro, "", "", false); err != nil {
			t.Fatalf("storeImage with excludeExtras: %v", err)
		}

		// Only the primary image must be present — no sigs, atts, or sboms.
		count := countArtifactsInStore(t, s)
		if count != 1 {
			t.Errorf("expected 1 artifact in store, got %d", count)
		}
		assertArtifactKindInStore(t, s, "test/signed:v1", consts.KindAnnotationImage)

		// Verify no sig/att/sbom kind annotations are present.
		for _, kind := range []string{consts.KindAnnotationSigs, consts.KindAnnotationAtts, consts.KindAnnotationSboms} {
			found := false
			if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
				if desc.Annotations[consts.KindAnnotationName] == kind {
					found = true
				}
				return nil
			}); err != nil {
				t.Fatalf("walk: %v", err)
			}
			if found {
				t.Errorf("unexpected artifact with kind %q found in store", kind)
			}
		}
	})

	t.Run("OCI 1.1 referrers excluded when excludeExtras=true", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)

		img := seedImage(t, host, "test/image", "v1", rOpts...)
		seedOCI11Referrer(t, host, "test/image", img, rOpts...)

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		if err := storeImage(ctx, s, v1.Image{Name: host + "/test/image:v1"}, "", true, rso, ro, "", "", false); err != nil {
			t.Fatalf("storeImage with excludeExtras: %v", err)
		}

		// Only the primary image must be present — no referrers.
		count := countArtifactsInStore(t, s)
		if count != 1 {
			t.Errorf("expected 1 artifact in store, got %d", count)
		}

		// Verify no referrer kind annotations are present.
		found := false
		if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
			if strings.HasPrefix(desc.Annotations[consts.KindAnnotationName], consts.KindAnnotationReferrers) {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatalf("walk: %v", err)
		}
		if found {
			t.Errorf("unexpected OCI referrer found in store when excludeExtras=true")
		}
	})

	t.Run("cosign v2 artifacts included when excludeExtras=false", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)

		img := seedImage(t, host, "test/signed", "v2", rOpts...)
		seedCosignV2Artifacts(t, host, "test/signed", img, rOpts...)

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		if err := storeImage(ctx, s, v1.Image{Name: host + "/test/signed:v2"}, "", false, rso, ro, "", "", false); err != nil {
			t.Fatalf("storeImage without excludeExtras: %v", err)
		}

		// All four artifacts (image + sig + att + sbom) must be present.
		assertArtifactKindInStore(t, s, "test/signed:v2", consts.KindAnnotationSigs)
		assertArtifactKindInStore(t, s, "test/signed:v2", consts.KindAnnotationAtts)
		assertArtifactKindInStore(t, s, "test/signed:v2", consts.KindAnnotationSboms)
	})
}

func TestAddChartCmd_LocalTgz(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	o := newAddChartOpts(chartTestdataDir, "")
	o.Concurrency = consts.DefaultConcurrency
	if err := AddChartCmd(ctx, o, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	// Hauler stores all artifacts (files, charts) via store.AddArtifact, which
	// unconditionally sets KindAnnotationName = KindAnnotationImage (see
	// pkg/store/store.go). There is no separate "chart" kind — charts are
	// wrapped in an OCI image manifest and tagged with KindAnnotationImage.
	assertArtifactKindInStore(t, s, "rancher-cluster-templates", consts.KindAnnotationImage)
}

func TestAddChartCmd_WithFileDep(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	o := newAddChartOpts(chartTestdataDir, "")
	o.Concurrency = consts.DefaultConcurrency
	if err := AddChartCmd(ctx, o, s, "chart-with-file-dependency-chart-1.0.0.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	assertArtifactInStore(t, s, "chart-with-file-dependency-chart")
}

// TestAddChartCmd_DependencyTreeAtVaryingConcurrency proves AddChartCmd
// resolves a chart's dependency tree correctly regardless of o.Concurrency.
// It reuses the file-dependency fixture (a parent with two dependencies
// resolved BFS-style) at both concurrency=1 -- forcing runChartJobs'
// single-worker path -- and a higher value, and checks the store ends up
// identical either way, under -race. It does NOT prove concurrency is
// "honored": nothing here observes actual fan-out or parallelism, so a
// hardcoded consts.DefaultConcurrency that silently ignored o.Concurrency
// would still pass both subtests unchanged.
// TestTraverseChartLevels_BoundedFanOut is the test that actually asserts
// on the concurrency bound.
func TestAddChartCmd_DependencyTreeAtVaryingConcurrency(t *testing.T) {
	for _, concurrency := range []int{1, 4} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			ctx := newTestContext(t)
			s := newTestStore(t)
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()

			o := newAddChartOpts(chartTestdataDir, "")
			o.AddDependencies = true
			o.Concurrency = concurrency

			if err := AddChartCmd(ctx, o, s, "chart-with-file-dependency-chart-1.0.0.tgz", rso, ro); err != nil {
				t.Fatalf("AddChartCmd concurrency=%d: %v", concurrency, err)
			}

			assertArtifactInStore(t, s, "chart-with-file-dependency-chart:1.0.0")
			assertArtifactInStore(t, s, "child:2.0.0")
			assertArtifactInStore(t, s, "crds:0.0.1")
		})
	}
}

func TestStoreChart_Rewrite(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	o := newAddChartOpts(chartTestdataDir, "")
	o.Rewrite = "myorg/custom-chart"

	if err := AddChartCmd(ctx, o, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd with rewrite: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/custom-chart")

	// OriginalRefAnnotation must retain the pre-rewrite "repoURL|repo:tag" encoding
	// (see encodeOriginalChartRef) regardless of the --rewrite applied above.
	wantPrefix := chartTestdataDir + "|"
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], "myorg/custom-chart") {
			original := desc.Annotations[consts.OriginalRefAnnotation]
			if !strings.HasPrefix(original, wantPrefix) {
				t.Errorf("OriginalRefAnnotation = %q, want prefix %q", original, wantPrefix)
			}
			if strings.Contains(original, "myorg/custom-chart") {
				t.Errorf("OriginalRefAnnotation = %q must not contain the rewritten ref", original)
			}
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !found {
		t.Fatal("expected to find rewritten chart artifact in store")
	}
}

// seedChartWithImages builds a minimal Helm chart whose helm.sh/images
// annotation lists the given image refs and saves it as a .tgz into dir.
// Returns the path to the saved .tgz file.
func seedChartWithImages(t *testing.T, dir string, images []string) string {
	t.Helper()

	// Build a helm.sh/images YAML list from the image refs.
	var sb strings.Builder
	for _, img := range images {
		sb.WriteString("- image: ")
		sb.WriteString(img)
		sb.WriteString("\n")
	}

	c := &helmchart.Chart{
		Metadata: &helmchart.Metadata{
			APIVersion: "v2",
			Name:       "test-chart",
			Version:    "0.1.0",
			Annotations: map[string]string{
				"helm.sh/images": sb.String(),
			},
		},
	}

	saved, err := util.Save(c, dir)
	if err != nil {
		t.Fatalf("seedChartWithImages: util.Save: %v", err)
	}
	return saved
}

func TestRunChartJobs_AddImages_ExcludeExtras(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	// Seed an image with cosign v2 artifacts (sig + att + sbom).
	img := seedImage(t, host, "test/chart-image", "v1", rOpts...)
	seedCosignV2Artifacts(t, host, "test/chart-image", img, rOpts...)

	// Build a minimal chart whose helm.sh/images annotation references the image.
	chartDir := t.TempDir()
	imageRef := host + "/test/chart-image:v1"
	tgzPath := seedChartWithImages(t, chartDir, []string{imageRef})

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	t.Run("excludeExtras=true suppresses sigs/atts/sboms for chart-discovered images", func(t *testing.T) {
		job := chartJob{
			cfg: v1.Chart{Name: tgzPath},
			opts: flags.AddChartOpts{
				ChartOpts:     newAddChartOpts("", "").ChartOpts,
				AddImages:     true,
				ExcludeExtras: true,
			},
		}
		if err := runChartJobs(ctx, s, []chartJob{job}, 1, rso, ro, nil); err != nil {
			t.Fatalf("runChartJobs with ExcludeExtras: %v", err)
		}

		// The chart itself is stored as an OCI image artifact.
		assertArtifactInStore(t, s, "test-chart")
		// The discovered image is stored (bare, no extras).
		assertArtifactInStore(t, s, "test/chart-image:v1")

		// No sig / att / sbom entries must be present.
		for _, kind := range []string{consts.KindAnnotationSigs, consts.KindAnnotationAtts, consts.KindAnnotationSboms} {
			found := false
			if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
				if desc.Annotations[consts.KindAnnotationName] == kind {
					found = true
				}
				return nil
			}); err != nil {
				t.Fatalf("walk: %v", err)
			}
			if found {
				t.Errorf("unexpected artifact with kind %q found in store when ExcludeExtras=true", kind)
			}
		}
	})
}

func TestRunChartJobs_AddImages_IncludeExtras(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	// Seed an image with cosign v2 artifacts.
	img := seedImage(t, host, "test/chart-image", "v2", rOpts...)
	seedCosignV2Artifacts(t, host, "test/chart-image", img, rOpts...)

	chartDir := t.TempDir()
	imageRef := host + "/test/chart-image:v2"
	tgzPath := seedChartWithImages(t, chartDir, []string{imageRef})

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	t.Run("excludeExtras=false includes sigs/atts/sboms for chart-discovered images", func(t *testing.T) {
		job := chartJob{
			cfg: v1.Chart{Name: tgzPath},
			opts: flags.AddChartOpts{
				ChartOpts:     newAddChartOpts("", "").ChartOpts,
				AddImages:     true,
				ExcludeExtras: false,
			},
		}
		if err := runChartJobs(ctx, s, []chartJob{job}, 1, rso, ro, nil); err != nil {
			t.Fatalf("runChartJobs without ExcludeExtras: %v", err)
		}

		assertArtifactKindInStore(t, s, "test/chart-image:v2", consts.KindAnnotationSigs)
		assertArtifactKindInStore(t, s, "test/chart-image:v2", consts.KindAnnotationAtts)
		assertArtifactKindInStore(t, s, "test/chart-image:v2", consts.KindAnnotationSboms)
	})
}

// --------------------------------------------------------------------------
// --local flag validation tests
// --------------------------------------------------------------------------

// TestRunChartJobs_AddImages_IgnoreErrors_EnvVar verifies that a
// chart-discovered image failure is swallowed via HAULER_IGNORE_ERRORS alone,
// without --ignore-errors, and that the chart itself is still stored.
// Regression test: retry.Operation and storeImage used to mutate the shared
// ro.IgnoreErrors from the env var as a side effect, so every ro.IgnoreErrors
// read after the first one saw the env var. Now that storeImage and
// retry.Operation are pure reads via flags.ShouldIgnoreErrors, fetchChart's
// image-discovery loop and the image phase must each honor the env var on
// their own.
func TestRunChartJobs_AddImages_IgnoreErrors_EnvVar(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("applyDefaultRegistry failure via env var", func(t *testing.T) {
		chartDir := t.TempDir()
		// Uppercase letters and spaces are rejected by reference.Parse, so this
		// only fails once opts.Registry is non-empty and applyDefaultRegistry
		// actually parses the ref.
		tgzPath := seedChartWithImages(t, chartDir, []string{"INVALID IMAGE REF !! ##"})

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()
		t.Setenv(consts.HaulerIgnoreErrors, "true")

		job := chartJob{
			cfg: v1.Chart{Name: tgzPath},
			opts: flags.AddChartOpts{
				ChartOpts: newAddChartOpts("", "").ChartOpts,
				AddImages: true,
				Registry:  "registry.example.com",
			},
		}
		if err := runChartJobs(ctx, s, []chartJob{job}, 1, rso, ro, nil); err != nil {
			t.Fatalf("expected nil with HAULER_IGNORE_ERRORS=true, got: %v", err)
		}
		if ro.IgnoreErrors {
			t.Fatal("expected ro.IgnoreErrors to remain false: the chart path must not mutate ro")
		}
		assertArtifactInStore(t, s, "test-chart")
	})

	t.Run("storeImage failure via env var", func(t *testing.T) {
		chartDir := t.TempDir()
		// localhost:1 is a port that is never listening.
		tgzPath := seedChartWithImages(t, chartDir, []string{"localhost:1/nonexistent/image:missing"})

		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()
		t.Setenv(consts.HaulerIgnoreErrors, "true")

		job := chartJob{
			cfg: v1.Chart{Name: tgzPath},
			opts: flags.AddChartOpts{
				ChartOpts: newAddChartOpts("", "").ChartOpts,
				AddImages: true,
			},
		}
		if err := runChartJobs(ctx, s, []chartJob{job}, 1, rso, ro, nil); err != nil {
			t.Fatalf("expected nil with HAULER_IGNORE_ERRORS=true, got: %v", err)
		}
		if ro.IgnoreErrors {
			t.Fatal("expected ro.IgnoreErrors to remain false: the chart path must not mutate ro")
		}
		assertArtifactInStore(t, s, "test-chart")
	})
}

func TestAddImageCmd_LocalFlagValidation(t *testing.T) {
	ctx := newTestContext(t)

	tests := []struct {
		name string
		opts *flags.AddImageOpts
	}{
		{
			name: "Local with Key returns error",
			opts: &flags.AddImageOpts{
				Local: true,
				Key:   "some.pub",
			},
		},
		{
			name: "Local with CertIdentity returns error",
			opts: &flags.AddImageOpts{
				Local:        true,
				CertIdentity: "foo@bar.com",
			},
		},
		{
			name: "Local with CertIdentityRegexp returns error",
			opts: &flags.AddImageOpts{
				Local:              true,
				CertIdentityRegexp: ".*",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()

			err := AddImageCmd(ctx, tc.opts, s, "nginx:latest", rso, ro)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "--local cannot be combined") {
				t.Errorf("expected '--local cannot be combined' in error, got: %v", err)
			}
		})
	}
}

// --------------------------------------------------------------------------
// AddImageCmd verification tests
// --------------------------------------------------------------------------

// `store add image` must store the digest it verified. Verifying the tag and
// then letting storeImage resolve it a second time leaves a window in which the
// tag can move, so the bytes stored are not the bytes checked -- the window
// `store sync` closes in resolveAndVerify.
//
// The request log is what distinguishes the two: pinning first means the tag is
// resolved exactly once, by the command's own pin. Comparing the stored digest
// alone would pass either way, since nothing moves the tag mid-test.
func TestAddImageCmd_StoresTheDigestItVerified(t *testing.T) {
	host, remoteOpts, rec := newRecordingRegistry(t)
	img, keyPath := seedSignedImage(t, host, "signed", "v1", remoteOpts...)
	want, err := img.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	rec.reset() // seeding itself HEADs the tag

	s := newTestStore(t)
	o := &flags.AddImageOpts{Key: keyPath, ExcludeExtras: true}
	if err := AddImageCmd(newTestContext(t), o, s, host+"/signed:v1", defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("AddImageCmd: %v", err)
	}

	got := storedDigest(t, s, "signed:v1")
	if got == "" {
		t.Fatalf("a validly signed image was not stored; requests:\n%v", rec.snapshot())
	}
	if got != want.String() {
		t.Fatalf("stored digest %s, want the verified digest %s", got, want)
	}
	if n := rec.countContaining("manifests/v1"); n != 1 {
		t.Fatalf("the tag was resolved %d times, want exactly 1 (the command's own pin); verification and the pull are each resolving it\nrequests:\n%v", n, rec.snapshot())
	}
}

// A signature that does not check out fails the command. `store sync` drops one
// image and carries on; `store add image` has only the one image, and returning
// success would leave the user believing an image had been verified when it was
// not stored at all.
func TestAddImageCmd_VerificationFailureFailsTheCommand(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	img := seedImage(t, host, "badsig", "v1", remoteOpts...)
	// The signature manifest exists but carries no usable signature.
	seedCosignV2Artifacts(t, host, "badsig", img, remoteOpts...)

	s := newTestStore(t)
	o := &flags.AddImageOpts{Key: writeTestPubKey(t), ExcludeExtras: true}
	if err := AddImageCmd(newTestContext(t), o, s, host+"/badsig:v1", defaultRootOpts(s.Root), defaultCliOpts()); err == nil {
		t.Fatal("AddImageCmd returned nil for an image whose only signature is unusable")
	}
	if got := countArtifactsInStore(t, s); got != 0 {
		t.Fatalf("store holds %d artifacts, want 0; the image that failed verification was stored anyway", got)
	}
}

// verifyAddImage must hand back the digest it pinned on every failure after
// the pin itself succeeded (verifier setup, signature check), so the caller
// can store exactly the bytes that were checked even though the check failed.
// A failure at or before the pin (malformed reference, unresolvable digest)
// has no digest to hand back and must return "".
func TestVerifyAddImage_ReturnsPinnedDigestOnPostPinFailureOnly(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	badsig := seedImage(t, host, "badsig", "v1", remoteOpts...)
	seedCosignV2Artifacts(t, host, "badsig", badsig, remoteOpts...)
	badsigDigest, err := badsig.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	tests := []struct {
		name       string
		ref        string
		o          *flags.AddImageOpts
		wantPinned string // "" means the failure happened at or before the pin
	}{
		{
			name:       "malformed reference",
			ref:        "NOT A REF",
			o:          &flags.AddImageOpts{Key: writeTestPubKey(t)},
			wantPinned: "",
		},
		{
			name:       "unreachable registry",
			ref:        "127.0.0.1:1/absent/image:v1",
			o:          &flags.AddImageOpts{Key: writeTestPubKey(t)},
			wantPinned: "",
		},
		{
			name:       "unreadable key",
			ref:        host + "/badsig:v1",
			o:          &flags.AddImageOpts{Key: filepath.Join(t.TempDir(), "missing.pub")},
			wantPinned: badsigDigest.String(),
		},
		{
			name:       "signature that does not check out",
			ref:        host + "/badsig:v1",
			o:          &flags.AddImageOpts{Key: writeTestPubKey(t)},
			wantPinned: badsigDigest.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rso, ro := defaultRootOpts(t.TempDir()), defaultCliOpts()
			pinned, err := verifyAddImage(newTestContext(t), tt.o, tt.ref, rso, ro)
			if err == nil {
				t.Fatal("verifyAddImage succeeded")
			}
			if pinned != tt.wantPinned {
				t.Fatalf("pinned digest = %q, want %q", pinned, tt.wantPinned)
			}
		})
	}
}

// Without --ignore-errors, AddImageCmd's behavior is unchanged by this
// feature -- see TestAddImageCmd_VerificationFailureFailsTheCommand. With it,
// the command must log the failure at WARN and store the image anyway,
// unverified, using whatever digest verifyAddImage already pinned -- the same
// tradeoff `store sync` makes for one image out of many, now available to the
// single-image command too.
func TestAddImageCmd_IgnoreErrors_StoresUnverifiedImage(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	img := seedImage(t, host, "badsig", "v1", remoteOpts...)
	seedCosignV2Artifacts(t, host, "badsig", img, remoteOpts...)
	ref := host + "/badsig:v1"

	s := newTestStore(t)
	o := &flags.AddImageOpts{Key: writeTestPubKey(t), ExcludeExtras: true}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	ro.IgnoreErrors = true

	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(context.Background())

	if err := AddImageCmd(ctx, o, s, ref, rso, ro); err != nil {
		t.Fatalf("AddImageCmd: %v", err)
	}

	// The assertion that matters most: an image that failed verification is
	// in the store anyway, unverified.
	assertArtifactInStore(t, s, "badsig:v1")

	out := buf.String()
	if !strings.Contains(out, "WRN") || !strings.Contains(out, ref) {
		t.Fatalf("expected a WARN line naming %q, got:\n%s", ref, out)
	}
}

// AddImageCmd's audit entry must report whether verification actually
// succeeded, not merely whether it was requested -- see
// TestRunImageJobs_AuditVerifiedFlag for the sync.go counterpart and the bug
// this guards.
func TestAddImageCmd_AuditVerifiedFlag(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	tests := []struct {
		name         string
		buildOpts    func(t *testing.T) (*flags.AddImageOpts, string)
		ignoreErrors bool
		want         bool
	}{
		{
			name: "not requested",
			buildOpts: func(t *testing.T) (*flags.AddImageOpts, string) {
				seedImage(t, host, "test/addaudit-unrequested", "v1", remoteOpts...)
				return &flags.AddImageOpts{ExcludeExtras: true}, host + "/test/addaudit-unrequested:v1"
			},
			want: false,
		},
		{
			name: "requested and passed",
			buildOpts: func(t *testing.T) (*flags.AddImageOpts, string) {
				_, keyPath := seedSignedImage(t, host, "test/addaudit-passed", "v1", remoteOpts...)
				return &flags.AddImageOpts{Key: keyPath, ExcludeExtras: true}, host + "/test/addaudit-passed:v1"
			},
			want: true,
		},
		{
			name: "requested and failed, stored anyway under --ignore-errors",
			buildOpts: func(t *testing.T) (*flags.AddImageOpts, string) {
				bad := seedImage(t, host, "test/addaudit-failed", "v1", remoteOpts...)
				seedCosignV2Artifacts(t, host, "test/addaudit-failed", bad, remoteOpts...)
				return &flags.AddImageOpts{Key: writeTestPubKey(t), ExcludeExtras: true}, host + "/test/addaudit-failed:v1"
			},
			ignoreErrors: true,
			want:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, ref := tc.buildOpts(t)

			s := newTestStore(t)
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()
			ro.AuditLevel = "verbose"
			ro.IgnoreErrors = tc.ignoreErrors
			ro.HaulerDir = t.TempDir()

			if err := AddImageCmd(newTestContext(t), o, s, ref, rso, ro); err != nil {
				t.Fatalf("AddImageCmd: %v", err)
			}

			flags := lastAuditEntryFlags(t, ro.HaulerDir)
			got, ok := flags["verified"].(bool)
			if !ok {
				t.Fatalf("audit entry's flags[\"verified\"] is %v (%T), want a bool", flags["verified"], flags["verified"])
			}
			if got != tc.want {
				t.Errorf("flags[\"verified\"] = %v, want %v", got, tc.want)
			}
		})
	}
}

// A key supplied alongside identity flags verifies against the key alone, as it
// always has. cosign.Config.validate rejects that pairing outright, so building
// the Config from the raw flags instead of from the branch this command selects
// would turn a working invocation into a hard error.
func TestAddImageCmd_KeyOutranksIdentityFlags(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	_, keyPath := seedSignedImage(t, host, "signed", "v1", remoteOpts...)

	s := newTestStore(t)
	o := &flags.AddImageOpts{
		Key:                keyPath,
		CertIdentity:       "someone@example.com",
		CertIdentityRegexp: ".*",
		ExcludeExtras:      true,
	}
	if err := AddImageCmd(newTestContext(t), o, s, host+"/signed:v1", defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("AddImageCmd rejected a key paired with identity flags: %v", err)
	}
	assertArtifactInStore(t, s, "signed:v1")
}

// --------------------------------------------------------------------------
// storeLocalImage unit tests
// --------------------------------------------------------------------------

func TestStoreLocalImage_InvalidReference(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("malformed reference returns error", func(t *testing.T) {
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		err := storeLocalImage(ctx, s, v1.Image{Name: "INVALID:::ref"}, rso, ro, "")
		if err == nil {
			t.Fatal("expected error for malformed reference, got nil")
		}
	})

	t.Run("malformed reference with IgnoreErrors returns nil", func(t *testing.T) {
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()
		ro.IgnoreErrors = true

		err := storeLocalImage(ctx, s, v1.Image{Name: "INVALID:::ref"}, rso, ro, "")
		if err != nil {
			t.Fatalf("expected nil with IgnoreErrors=true, got: %v", err)
		}
	})

	t.Run("malformed reference with HAULER_IGNORE_ERRORS env var returns nil and does not mutate ro", func(t *testing.T) {
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		t.Setenv(consts.HaulerIgnoreErrors, "true")

		err := storeLocalImage(ctx, s, v1.Image{Name: "INVALID:::ref"}, rso, ro, "")
		if err != nil {
			t.Fatalf("expected nil with HAULER_IGNORE_ERRORS=true, got: %v", err)
		}
		if ro.IgnoreErrors {
			t.Fatal("expected ro.IgnoreErrors to remain false: storeLocalImage must not mutate ro")
		}
	})
}

// --------------------------------------------------------------------------
// Durable index save tests
// --------------------------------------------------------------------------

// add_durable_index_test.go covers the durability gap identified in the
// final cross-task review of the I/O tuning workstream: index.json's
// per-artifact fsync was replaced with a 30-second durable checkpoint
// (content.OCI.saveIndexCheckpointLocked), so nothing below the command
// entry points forces an fsync any more. Ending a run durably is now the
// job of each entry point's deferred SaveIndex() -- AddFileCmd, AddImageCmd,
// and AddChartCmd each have one. Without it a completed `hauler store add
// image`/`add file`/`add chart` could return success with its final
// index.json state sitting only in page cache, not fsynced -- a
// lost-index-entry risk on crash/power-loss (blobs are unaffected;
// writeBlobOnce always fsyncs).
//
// These tests observe durability via content.OCI.Stats().Snapshot()'s
// IndexDurableWrites counter rather than by inspecting the filesystem
// directly, since "was this fsync'd" isn't otherwise observable from
// outside the package.

// TestAddImageCmd_EndsWithDurableIndexSave reproduces the gap for
// AddImageCmd. store.Layout.AddImage calls content.OCI.AddIndex once for
// the base image, then once more per discovered cosign signature,
// attestation, and SBOM (see saveRelatedArtifacts) -- all against the same
// store instance, well within the 30s checkpoint window. Because
// content.OCI's lastDurableSave starts at its zero value, Since(zero) is
// always >= 30s, so the very first AddIndex call of a fresh store is
// durable "for free" -- but every subsequent call, including the last one
// that actually reflects the complete set of discovered artifacts, is not.
// A fix must add a trailing durable SaveIndex() so the run always ends
// durable regardless of how many AddIndex calls happened inside it.
func TestAddImageCmd_EndsWithDurableIndexSave(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	host, remoteOpts := newLocalhostRegistry(t)
	img := seedImage(t, host, "myorg/durable", "v1", remoteOpts...)
	seedCosignV2Artifacts(t, host, "myorg/durable", img, remoteOpts...)

	o := &flags.AddImageOpts{}
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	ref := fmt.Sprintf("%s/myorg/durable:v1", host)
	if err := AddImageCmd(ctx, o, s, ref, rso, ro); err != nil {
		t.Fatalf("AddImageCmd: %v", err)
	}

	snap := s.OCI.Stats().Snapshot()
	// Without the fix this is 1: only the very first AddIndex call (image
	// itself) lands durably, courtesy of lastDurableSave's zero value. The
	// sig/att/sbom AddIndex calls that follow -- and the run's final state
	// -- are not durable until a trailing SaveIndex() is added.
	if snap.IndexDurableWrites < 2 {
		t.Fatalf("expected at least 2 durable index writes (initial AddIndex + trailing checkpoint), got %d (total index writes=%d)", snap.IndexDurableWrites, snap.IndexWrites)
	}
}

// TestAddFileCmd_EndsWithDurableIndexSave verifies AddFileCmd also ends its
// run with a durable index save. A single AddFileCmd call only performs one
// AddIndex call, which (per TestAddImageCmd_EndsWithDurableIndexSave's
// rationale) is already durable "for free" on a brand new store -- so this
// test seeds an unrelated file first to consume that free durability and
// set lastDurableSave to "now", then immediately adds a second file so its
// AddIndex call falls inside the 30s checkpoint window and would be
// non-durable without the fix.
func TestAddFileCmd_EndsWithDurableIndexSave(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	warmup, err := os.CreateTemp(t.TempDir(), "warmup-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	warmup.WriteString("warmup content") //nolint:errcheck
	warmup.Close()

	warmupOpts := &flags.AddFileOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := AddFileCmd(ctx, warmupOpts, s, warmup.Name(), defaultCliOpts()); err != nil {
		t.Fatalf("AddFileCmd warmup: %v", err)
	}

	tmp, err := os.CreateTemp(t.TempDir(), "durable-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("durable content") //nolint:errcheck
	tmp.Close()

	before := s.OCI.Stats().Snapshot().IndexDurableWrites

	o := &flags.AddFileOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := AddFileCmd(ctx, o, s, tmp.Name(), defaultCliOpts()); err != nil {
		t.Fatalf("AddFileCmd: %v", err)
	}

	after := s.OCI.Stats().Snapshot().IndexDurableWrites
	if after <= before {
		t.Fatalf("expected a trailing durable index save after AddFileCmd, durable writes went from %d to %d", before, after)
	}
}

// --------------------------------------------------------------------------
// File retry tests
// --------------------------------------------------------------------------

// add_file_retry_test.go covers storeFile's (cmd/hauler/cli/store/add.go)
// retry, --ignore-errors, and cancellation behavior -- extending it to match
// storeImage's existing shape (see add_retry_stats_test.go for the analogous
// image-side retry test) as part of bringing Files up to parity with Images
// for `hauler store sync`'s --concurrency support.

// TestStoreFile_RetriesOnTransientFailure proves storeFile retries a failed
// fetch via retry.Operation rather than aborting the whole sync on one
// transient HTTP blip -- storeFile previously had no retry wrapping at all.
func TestStoreFile_RetriesOnTransientFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}

	var gets int32
	mux := http.NewServeMux()
	mux.HandleFunc("/flaky.sh", func(w http.ResponseWriter, r *http.Request) {
		// storeFile's Client.Name(fi.Path) call (used to derive the stored
		// ref, before retry.Operation ever starts) issues an unconditional
		// HEAD request -- see getter.Http.Name -- that must not count
		// against the GET-failure budget below, or the "failure" would be
		// silently consumed before AddArtifact's first real attempt.
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		if atomic.AddInt32(&gets, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "#!/bin/sh\necho ok") //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newTestContext(t)
	s := newTestStore(t)

	rso := defaultRootOpts(s.Root)
	rso.Retries = 2 // one failed attempt + one successful retry
	ro := defaultCliOpts()

	if err := storeFile(ctx, s, v1.File{Path: srv.URL + "/flaky.sh"}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}
	assertArtifactInStore(t, s, "flaky.sh")
	// The first GET fails; layer.FromOpener opens once per successful
	// LayerFrom call (it derives diffID from the already-computed digest
	// instead of a second read), so a successful retry attempt adds 1 more
	// -- 2 total.
	if got := atomic.LoadInt32(&gets); got < 2 {
		t.Errorf("expected at least 2 GET attempts (1 failure + 1 for the succeeding retry), got %d", got)
	}
}

// TestStoreFile_IgnoreErrors_WarnsAndReturnsNil proves storeFile absorbs a
// failure into a warning and returns nil when --ignore-errors is set,
// matching storeImage's existing behavior -- previously storeFile always
// propagated the error regardless of ignore-errors.
func TestStoreFile_IgnoreErrors_WarnsAndReturnsNil(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	ro := defaultCliOpts()
	ro.IgnoreErrors = true
	rso := defaultRootOpts(s.Root)

	err := storeFile(ctx, s, v1.File{Path: "/nonexistent/path/missing-file.txt"}, ro, rso)
	if err != nil {
		t.Fatalf("expected nil error with --ignore-errors, got: %v", err)
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after an ignored failure, got %d", n)
	}
}

// TestStoreFile_ContextAlreadyCancelled_ReturnsPromptly is the regression
// test for File.compute()'s context.TODO() bug (pkg/artifacts/file/file.go):
// storeFile must check ctx and bail out before ever attempting to fetch,
// matching storeImage's early ctx.Err() check.
func TestStoreFile_ContextAlreadyCancelled_ReturnsPromptly(t *testing.T) {
	s := newTestStore(t)

	zl := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(zl.WithContext(context.Background()))
	cancel()

	ro := defaultCliOpts()
	rso := defaultRootOpts(s.Root)

	err := storeFile(ctx, s, v1.File{Path: "https://example.invalid/never-fetched.sh"}, ro, rso)
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context, got nil")
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts, got %d", n)
	}
}

// TestStoreFile_CompletionLine_Format proves storeFile logs a "✓ added"
// completion line (matching storeImage's formatAddedLine convention) rather
// than the old plain "successfully added file" line, and that the old
// "adding file" line is demoted to debug (absent at the default/error log
// level defaultCliOpts() uses).
func TestStoreFile_CompletionLine_Format(t *testing.T) {
	url := seedFileInHTTPServer(t, "completion.sh", "#!/bin/sh\necho done")

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf).Level(zerolog.InfoLevel)
	ctx := zl.WithContext(context.Background())

	ro := defaultCliOpts()
	ro.LogLevel = "info"
	rso := defaultRootOpts(s.Root)

	if err := storeFile(ctx, s, v1.File{Path: url}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "✓ added") {
		t.Errorf("expected a \"✓ added\" completion line, got:\n%s", out)
	}
	if strings.Contains(out, "successfully added file") {
		t.Errorf("expected the old \"successfully added file\" line to be gone, got:\n%s", out)
	}
}

// --------------------------------------------------------------------------
// Image retry stats tests
// --------------------------------------------------------------------------

// add_retry_stats_test.go covers a single narrow bug in storeImage
// (cmd/hauler/cli/store/add.go): a retried s.AddImage attempt must not
// accumulate layer count / byte totals onto the same store.ImageStats
// pointer used by a prior, failed attempt -- otherwise the "✓ added ..."
// completion line double (or N-x) counts layers/bytes across retries.

// failOnceForDigestHandler wraps an http.Handler and fails the very first GET
// request for one specific blob digest with a 403, then lets every other
// request -- including every later request for that same digest -- through
// unmodified. Targeting a single, specific digest (rather than "the first
// request for whichever digest shows up first") is deliberate: writeImageBlobs
// fetches an image's layers concurrently and its config sequentially
// afterward, so a naive "fail every digest's first-ever request" approach
// makes the *retry* attempt also fail (on whichever blob it reaches first
// that the earlier, aborted attempt never got around to requesting at all).
// Failing exactly one predetermined digest, exactly once, guarantees the
// first AddImage attempt fails (on that blob) while the second, retried
// attempt succeeds outright (every blob, including the previously-failing
// one, now passes through).
//
// 403 (rather than 500/502/503/504/408/429) is deliberate: go-containerregistry's
// remote transport has its own built-in retry for a fixed set of "temporary"
// status codes (see remote.defaultRetryStatusCodes) and would silently absorb
// a transient 500 within a single AddImage call (up to 3 internal attempts).
// 403 isn't in that set, so it surfaces as a hard error from that AddImage
// call and exercises this package's own retry.Operation-driven retry instead.
type failOnceForDigestHandler struct {
	next   http.Handler
	target string // e.g. "sha256:abcd..."

	mu     sync.Mutex
	failed bool
}

func (f *failOnceForDigestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/blobs/"+f.target) {
		f.mu.Lock()
		alreadyFailed := f.failed
		f.failed = true
		f.mu.Unlock()

		if !alreadyFailed {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	f.next.ServeHTTP(w, r)
}

// newFailOnceRegistry starts an in-memory OCI registry, listening on
// "localhost:0" (so go-containerregistry auto-selects plain HTTP, matching
// newLocalhostRegistry in add_test.go), wrapped in a failOnceForDigestHandler.
// The returned handler's target field must be set (to the digest that should
// fail exactly once) before the read that should fail; that happens after
// seeding, once the seeded image's layer digest is known, and before the
// caller reads the image back via AddImage/storeImage.
func newFailOnceRegistry(t *testing.T) (host string, remoteOpts []remote.Option, handler *failOnceForDigestHandler) {
	t.Helper()
	handler = &failOnceForDigestHandler{next: registry.New()}

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("newFailOnceRegistry listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts, handler
}

// addedLineLayerCount extracts the layer count from the last "✓ added ...
// (N layer(s), ...)" line in out. Fails the test if no such line is found.
func addedLineLayerCount(t *testing.T, out string) int {
	t.Helper()
	re := regexp.MustCompile(`✓ added .*\((\d+) layers?,`)
	matches := re.FindAllStringSubmatch(out, -1)
	if len(matches) == 0 {
		t.Fatalf("no \"✓ added ... (N layer(s), ...)\" line found in output:\n%s", out)
	}
	last := matches[len(matches)-1]
	n := 0
	for _, c := range last[1] {
		n = n*10 + int(c-'0')
	}
	return n
}

// TestStoreImage_RetryDoesNotDoubleCountStats is the regression test for the
// ImageStats double-counting bug: storeImage built a single store.ImageStats
// pointer outside retry.Operation's closure, so a failed first attempt that
// had already accumulated layer/byte counts into it left those counts in
// place for a subsequent, successful retry attempt to accumulate on top of --
// inflating the final completion line's layer/byte totals.
//
// seedImage (testhelpers_test.go) always creates a 2-layer image. A single,
// uncontested successful AddImage call reports "2 layers"; the bug would
// report "4 layers" after exactly one failed attempt followed by one
// successful retry.
func TestStoreImage_RetryDoesNotDoubleCountStats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}

	host, remoteOpts, handler := newFailOnceRegistry(t)
	seededImg := seedImage(t, host, "test/retry-stats", "v1", remoteOpts...)

	layers, err := seededImg.Layers()
	if err != nil {
		t.Fatalf("seededImg.Layers: %v", err)
	}
	if len(layers) == 0 {
		t.Fatal("seeded image has no layers")
	}
	targetDigest, err := layers[0].Digest()
	if err != nil {
		t.Fatalf("layers[0].Digest: %v", err)
	}
	handler.target = targetDigest.String()

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	ctx := zl.WithContext(context.Background())

	rso := defaultRootOpts(s.Root)
	rso.Retries = 2 // one failed attempt + one successful retry
	ro := defaultCliOpts()

	cfg := v1.Image{Name: host + "/test/retry-stats:v1"}
	if err := storeImage(ctx, s, cfg, "", true /* excludeExtras: keep this to just the image's own layers */, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	out := buf.String()
	if got := addedLineLayerCount(t, out); got != 2 {
		t.Errorf("reported layer count = %d, want 2 (seedImage's fixed layer count); a retried attempt must not double-count onto the prior failed attempt's stats\nfull output:\n%s", got, out)
	}
}

// --------------------------------------------------------------------------
// resolveChartJobs tests
//
// These exercise the Charts precedence rules directly, without a store or
// network access. Credential resolution (UsernameEnv/PasswordEnv and the
// field passthrough onto ChartOpts) is covered separately by
// TestResolveChartJobs_CredentialFields, TestResolveChartJobs_CredentialEnv,
// TestResolveChartJobs_CredentialEnvMismatch, and
// TestResolveChartJobs_CredentialIsolation below.
// --------------------------------------------------------------------------

func TestResolveChartJobs_Registry(t *testing.T) {
	tests := []struct {
		name        string
		cliRegistry string
		annotation  string
		want        string
	}{
		{
			name:        "CLI flag wins over annotation",
			cliRegistry: "cli-registry.io",
			annotation:  "annotation-registry.io",
			want:        "cli-registry.io",
		},
		{
			name:       "annotation used when no CLI flag",
			annotation: "annotation-registry.io",
			want:       "annotation-registry.io",
		},
		{
			name:        "CLI flag used when no annotation",
			cliRegistry: "cli-registry.io",
			want:        "cli-registry.io",
		},
		{
			name: "neither set leaves registry empty",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Registry: tc.cliRegistry}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationRegistry] = tc.annotation
			}

			jobs, err := resolveChartJobs(o, a, "/manifests", []v1.Chart{{Name: "rancher"}})
			if err != nil {
				t.Fatalf("resolveChartJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if got := jobs[0].opts.Registry; got != tc.want {
				t.Errorf("registry = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveChartJobs_ExcludeExtras(t *testing.T) {
	tests := []struct {
		name       string
		cli        bool
		cliChanged bool
		annotation string
		perChart   bool
		want       bool
	}{
		{name: "nothing set", want: false},
		{name: "CLI flag alone", cli: true, cliChanged: true, want: true},
		{name: "annotation alone", annotation: "true", want: true},
		{name: "per-chart alone", perChart: true, want: true},
		{
			name:       "annotation only honored when set to the literal true",
			annotation: "yes",
			want:       false,
		},
		{
			// Neither the annotation nor the per-chart field can turn a CLI
			// --exclude-extras back off; both are one-way switches.
			name:       "CLI flag survives an annotation that is not true",
			cli:        true,
			cliChanged: true,
			annotation: "false",
			want:       true,
		},
		{
			name:       "CLI flag survives a false per-chart field",
			cli:        true,
			cliChanged: true,
			perChart:   false,
			want:       true,
		},
		{
			name:       "annotation survives a false per-chart field",
			annotation: "true",
			perChart:   false,
			want:       true,
		},
		{
			// An explicit CLI --exclude-extras=false wins outright over an
			// annotation/per-chart true.
			name:       "explicit CLI false overrides annotation and per-chart",
			cliChanged: true,
			annotation: "true",
			perChart:   true,
			want:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{ExcludeExtras: tc.cli, ExcludeExtrasChanged: tc.cliChanged}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationExcludeExtras] = tc.annotation
			}

			jobs, err := resolveChartJobs(o, a, "/manifests", []v1.Chart{{Name: "rancher", ExcludeExtras: tc.perChart}})
			if err != nil {
				t.Fatalf("resolveChartJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if got := jobs[0].opts.ExcludeExtras; got != tc.want {
				t.Errorf("excludeExtras = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveChartJobs_Platform(t *testing.T) {
	tests := []struct {
		name       string
		cli        string
		annotation string
		perChart   string
		want       string
	}{
		{name: "nothing set", want: ""},
		{name: "CLI flag alone", cli: "linux/amd64", want: "linux/amd64"},
		{name: "annotation alone", annotation: "linux/arm64", want: "linux/arm64"},
		{name: "per-chart alone", perChart: "linux/s390x", want: "linux/s390x"},
		{
			// An explicit --platform is run-time intent and outranks manifest
			// metadata, matching resolveImageJobs and matching registry's
			// CLI-over-annotation rule in this same resolver.
			name:       "CLI flag wins over annotation",
			cli:        "linux/amd64",
			annotation: "linux/arm64",
			want:       "linux/amd64",
		},
		{
			name:       "CLI flag wins over annotation and per-chart",
			cli:        "linux/amd64",
			annotation: "linux/arm64",
			perChart:   "linux/s390x",
			want:       "linux/amd64",
		},
		{
			name:       "per-chart wins over annotation when CLI flag unset",
			annotation: "linux/arm64",
			perChart:   "linux/s390x",
			want:       "linux/s390x",
		},
		{
			name:       "empty annotation leaves the CLI flag intact",
			cli:        "linux/amd64",
			annotation: "",
			want:       "linux/amd64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Platform: tc.cli}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationPlatform] = tc.annotation
			}

			jobs, err := resolveChartJobs(o, a, "/manifests", []v1.Chart{{Name: "rancher", Platform: tc.perChart}})
			if err != nil {
				t.Fatalf("resolveChartJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			if got := jobs[0].opts.Platform; got != tc.want {
				t.Errorf("platform = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveChartJobs_PlatformMatchesImagePath pins the two resolvers
// together: a manifest carrying a platform annotation must select the same
// platform for its Charts and its Images given the same flags. The two
// resolvers hold separate copies of this precedence chain, and drift between
// them is exactly the bug this pins against.
func TestResolveChartJobs_PlatformMatchesImagePath(t *testing.T) {
	tests := []struct {
		name       string
		cli        string
		annotation string
		perEntry   string
	}{
		{name: "nothing set"},
		{name: "CLI flag alone", cli: "linux/amd64"},
		{name: "annotation alone", annotation: "linux/arm64"},
		{name: "per-entry alone", perEntry: "linux/s390x"},
		{name: "CLI flag and annotation", cli: "linux/amd64", annotation: "linux/arm64"},
		{name: "per-entry and annotation", annotation: "linux/arm64", perEntry: "linux/s390x"},
		{name: "per-entry and CLI flag", cli: "linux/amd64", perEntry: "linux/s390x"},
		{name: "all three", cli: "linux/amd64", annotation: "linux/arm64", perEntry: "linux/s390x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{Platform: tc.cli}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationPlatform] = tc.annotation
			}

			chartJobs, err := resolveChartJobs(o, a, "/manifests", []v1.Chart{{Name: "rancher", Platform: tc.perEntry}})
			if err != nil {
				t.Fatalf("resolveChartJobs: %v", err)
			}
			if len(chartJobs) != 1 {
				t.Fatalf("expected 1 chart job, got %d", len(chartJobs))
			}

			imageJobs, err := resolveImageJobs(o, a, []v1.Image{{Name: "rancher/rancher:v2.9", Platform: tc.perEntry}})
			if err != nil {
				t.Fatalf("resolveImageJobs: %v", err)
			}
			if len(imageJobs) != 1 {
				t.Fatalf("expected 1 image job, got %d", len(imageJobs))
			}

			if chartJobs[0].opts.Platform != imageJobs[0].platform {
				t.Errorf("chart platform = %q, image platform = %q, want them equal",
					chartJobs[0].opts.Platform, imageJobs[0].platform)
			}
		})
	}
}

func TestResolveChartJobs_NilAnnotations(t *testing.T) {
	o := &flags.SyncOpts{Registry: "cli-registry.io", Platform: "linux/amd64", ExcludeExtras: true}

	jobs, err := resolveChartJobs(o, nil, "/manifests", []v1.Chart{{Name: "rancher"}})
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if got := jobs[0].opts.Registry; got != "cli-registry.io" {
		t.Errorf("registry = %q, want cli-registry.io", got)
	}
	if got := jobs[0].opts.Platform; got != "linux/amd64" {
		t.Errorf("platform = %q, want linux/amd64", got)
	}
	if !jobs[0].opts.ExcludeExtras {
		t.Error("excludeExtras = false, want true")
	}
}

func TestResolveChartJobs_ValuesFiles(t *testing.T) {
	tests := []struct {
		name        string
		manifestDir string
		valuesFiles []string
		want        []string
	}{
		{
			name:        "relative paths join against the manifest directory",
			manifestDir: "/manifests",
			valuesFiles: []string{"values.yaml", "overrides/extra.yaml"},
			want:        []string{"/manifests/values.yaml", "/manifests/overrides/extra.yaml"},
		},
		{
			name:        "parent-relative paths are cleaned",
			manifestDir: "/manifests/prod",
			valuesFiles: []string{"../shared/values.yaml"},
			want:        []string{"/manifests/shared/values.yaml"},
		},
		{
			name:        "manifest in the current directory",
			manifestDir: ".",
			valuesFiles: []string{"values.yaml"},
			want:        []string{"values.yaml"},
		},
		{
			name:        "no values files yields none",
			manifestDir: "/manifests",
			valuesFiles: nil,
			want:        nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, tc.manifestDir,
				[]v1.Chart{{Name: "rancher", ValuesFiles: tc.valuesFiles}})
			if err != nil {
				t.Fatalf("resolveChartJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}

			got := jobs[0].opts.ValuesFiles
			if len(got) != len(tc.want) {
				t.Fatalf("valuesFiles = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("valuesFiles[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestResolveChartJobs_PerChartFields(t *testing.T) {
	charts := []v1.Chart{
		{
			Name:            "rancher",
			RepoURL:         "https://releases.rancher.com/server-charts/stable",
			Version:         "2.9.0",
			Rewrite:         "mirror/rancher",
			AddImages:       true,
			AddDependencies: true,
		},
		{
			Name:    "cert-manager",
			RepoURL: "https://charts.jetstack.io",
			Version: "1.15.0",
		},
	}

	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", charts)
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	if jobs[0].cfg.Name != "rancher" || jobs[1].cfg.Name != "cert-manager" {
		t.Fatalf("jobs are out of manifest order: %q, %q", jobs[0].cfg.Name, jobs[1].cfg.Name)
	}

	if jobs[0].rewrite != "mirror/rancher" {
		t.Errorf("jobs[0].rewrite = %q, want mirror/rancher", jobs[0].rewrite)
	}
	if jobs[1].rewrite != "" {
		t.Errorf("jobs[1].rewrite = %q, want empty", jobs[1].rewrite)
	}
	if !jobs[0].opts.AddImages || !jobs[0].opts.AddDependencies {
		t.Errorf("jobs[0] add-images/add-dependencies = %v/%v, want true/true", jobs[0].opts.AddImages, jobs[0].opts.AddDependencies)
	}
	if jobs[1].opts.AddImages || jobs[1].opts.AddDependencies {
		t.Errorf("jobs[1] add-images/add-dependencies = %v/%v, want false/false", jobs[1].opts.AddImages, jobs[1].opts.AddDependencies)
	}

	for i, want := range charts {
		if got := jobs[i].opts.ChartOpts.RepoURL; got != want.RepoURL {
			t.Errorf("jobs[%d].opts.ChartOpts.RepoURL = %q, want %q", i, got, want.RepoURL)
		}
		if got := jobs[i].opts.ChartOpts.Version; got != want.Version {
			t.Errorf("jobs[%d].opts.ChartOpts.Version = %q, want %q", i, got, want.Version)
		}
		if jobs[i].parent != "" {
			t.Errorf("jobs[%d].parent = %q, want empty for a top-level chart", i, jobs[i].parent)
		}
		if jobs[i].depth != 0 {
			t.Errorf("jobs[%d].depth = %d, want 0 for a top-level chart", i, jobs[i].depth)
		}
	}
}

// TestResolveChartJobs_ChartOptsNotShared is the regression test for the
// shallow AddChartOpts copy: every job must own its *action.ChartPathOptions,
// so mutating one job's RepoURL/Version cannot leak into a sibling's.
func TestResolveChartJobs_ChartOptsNotShared(t *testing.T) {
	charts := []v1.Chart{
		{Name: "rancher", RepoURL: "https://releases.rancher.com/server-charts/stable", Version: "2.9.0"},
		{Name: "cert-manager", RepoURL: "https://charts.jetstack.io", Version: "1.15.0"},
		{Name: "longhorn", RepoURL: "https://charts.longhorn.io", Version: "1.7.0"},
	}

	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", charts)
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != len(charts) {
		t.Fatalf("expected %d jobs, got %d", len(charts), len(jobs))
	}

	for i := range jobs {
		for k := i + 1; k < len(jobs); k++ {
			if jobs[i].opts.ChartOpts == jobs[k].opts.ChartOpts {
				t.Fatalf("jobs[%d] and jobs[%d] share one *action.ChartPathOptions pointee", i, k)
			}
		}
	}

	jobs[0].opts.ChartOpts.RepoURL = "file:///tmp/subchart"
	jobs[0].opts.ChartOpts.Version = "0.0.0-mutated"

	for i := 1; i < len(jobs); i++ {
		if got := jobs[i].opts.ChartOpts.RepoURL; got != charts[i].RepoURL {
			t.Errorf("jobs[%d].opts.ChartOpts.RepoURL = %q, want %q", i, got, charts[i].RepoURL)
		}
		if got := jobs[i].opts.ChartOpts.Version; got != charts[i].Version {
			t.Errorf("jobs[%d].opts.ChartOpts.Version = %q, want %q", i, got, charts[i].Version)
		}
	}
}

func TestResolveChartJobs_NoCharts(t *testing.T) {
	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", nil)
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %d", len(jobs))
	}
}

// TestResolveChartJobs_CredentialFields pins that every TLS/verification
// field on v1.Chart reaches the job's ChartOpts unchanged.
func TestResolveChartJobs_CredentialFields(t *testing.T) {
	ch := v1.Chart{
		Name:                  "rancher",
		Verify:                true,
		Keyring:               "/keys/pubring.gpg",
		PassCredentialsAll:    true,
		CertFile:              "/certs/client.crt",
		KeyFile:               "/certs/client.key",
		CaFile:                "/certs/ca.crt",
		InsecureSkipTLSVerify: true,
		PlainHTTP:             true,
	}

	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", []v1.Chart{ch})
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	opts := jobs[0].opts.ChartOpts
	if opts.Verify != ch.Verify {
		t.Errorf("Verify = %v, want %v", opts.Verify, ch.Verify)
	}
	if opts.Keyring != ch.Keyring {
		t.Errorf("Keyring = %q, want %q", opts.Keyring, ch.Keyring)
	}
	if opts.PassCredentialsAll != ch.PassCredentialsAll {
		t.Errorf("PassCredentialsAll = %v, want %v", opts.PassCredentialsAll, ch.PassCredentialsAll)
	}
	if opts.CertFile != ch.CertFile {
		t.Errorf("CertFile = %q, want %q", opts.CertFile, ch.CertFile)
	}
	if opts.KeyFile != ch.KeyFile {
		t.Errorf("KeyFile = %q, want %q", opts.KeyFile, ch.KeyFile)
	}
	if opts.CaFile != ch.CaFile {
		t.Errorf("CaFile = %q, want %q", opts.CaFile, ch.CaFile)
	}
	if opts.InsecureSkipTLSVerify != ch.InsecureSkipTLSVerify {
		t.Errorf("InsecureSkipTLSVerify = %v, want %v", opts.InsecureSkipTLSVerify, ch.InsecureSkipTLSVerify)
	}
	if opts.PlainHTTP != ch.PlainHTTP {
		t.Errorf("PlainHTTP = %v, want %v", opts.PlainHTTP, ch.PlainHTTP)
	}
}

func TestResolveChartJobs_CaFilePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		cli        string
		annotation string
		perChart   string
		want       string
	}{
		{name: "annotation used when CLI and per-chart unset", annotation: "/ann/ca.crt", want: "/ann/ca.crt"},
		{name: "per-chart wins over annotation", annotation: "/ann/ca.crt", perChart: "/chart/ca.crt", want: "/chart/ca.crt"},
		{name: "CLI wins over per-chart and annotation", cli: "/cli/ca.crt", annotation: "/ann/ca.crt", perChart: "/chart/ca.crt", want: "/cli/ca.crt"},
		{name: "none set stays empty", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := &flags.SyncOpts{CaFile: tc.cli}
			a := map[string]string{}
			if tc.annotation != "" {
				a[consts.ImageAnnotationCaFile] = tc.annotation
			}

			jobs, err := resolveChartJobs(o, a, "/manifests", []v1.Chart{{Name: "rancher", CaFile: tc.perChart}})
			if err != nil {
				t.Fatalf("resolveChartJobs: %v", err)
			}
			if got := jobs[0].opts.ChartOpts.CaFile; got != tc.want {
				t.Errorf("CaFile = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveChartJobs_CredentialEnv pins that UsernameEnv/PasswordEnv are
// resolved into ChartOpts.Username/Password via resolveChartCreds.
func TestResolveChartJobs_CredentialEnv(t *testing.T) {
	t.Setenv("CHART_USER", "alice")
	t.Setenv("CHART_PASS", "s3cret")

	ch := v1.Chart{Name: "rancher", UsernameEnv: "CHART_USER", PasswordEnv: "CHART_PASS"}

	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", []v1.Chart{ch})
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if got := jobs[0].opts.ChartOpts.Username; got != "alice" {
		t.Errorf("Username = %q, want alice", got)
	}
	if got := jobs[0].opts.ChartOpts.Password; got != "s3cret" {
		t.Errorf("Password = %q, want s3cret", got)
	}
}

// TestResolveChartJobs_CredentialEnvMismatch pins the fail-fast contract: a
// chart with only one of UsernameEnv/PasswordEnv set must abort the whole
// resolve with no jobs returned, not just skip credentials for that chart.
func TestResolveChartJobs_CredentialEnvMismatch(t *testing.T) {
	ch := v1.Chart{Name: "rancher", UsernameEnv: "CHART_USER"}

	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", []v1.Chart{ch})
	if err == nil {
		t.Fatal("expected an error for a chart with usernameEnv set but passwordEnv empty")
	}
	if jobs != nil {
		t.Errorf("expected nil jobs on error, got %d", len(jobs))
	}
}

// TestResolveChartJobs_CredentialIsolation is the credential analogue of
// TestResolveChartJobs_ChartOptsNotShared: each job's Username/Password must
// come from that chart's own env vars, not a sibling's.
func TestResolveChartJobs_CredentialIsolation(t *testing.T) {
	t.Setenv("RANCHER_USER", "rancher-user")
	t.Setenv("RANCHER_PASS", "rancher-pass")
	t.Setenv("LONGHORN_USER", "longhorn-user")
	t.Setenv("LONGHORN_PASS", "longhorn-pass")

	charts := []v1.Chart{
		{Name: "rancher", UsernameEnv: "RANCHER_USER", PasswordEnv: "RANCHER_PASS"},
		{Name: "longhorn", UsernameEnv: "LONGHORN_USER", PasswordEnv: "LONGHORN_PASS"},
	}

	jobs, err := resolveChartJobs(&flags.SyncOpts{}, nil, "/manifests", charts)
	if err != nil {
		t.Fatalf("resolveChartJobs: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}

	if got := jobs[0].opts.ChartOpts.Username; got != "rancher-user" {
		t.Errorf("jobs[0].opts.ChartOpts.Username = %q, want rancher-user", got)
	}
	if got := jobs[0].opts.ChartOpts.Password; got != "rancher-pass" {
		t.Errorf("jobs[0].opts.ChartOpts.Password = %q, want rancher-pass", got)
	}
	if got := jobs[1].opts.ChartOpts.Username; got != "longhorn-user" {
		t.Errorf("jobs[1].opts.ChartOpts.Username = %q, want longhorn-user", got)
	}
	if got := jobs[1].opts.ChartOpts.Password; got != "longhorn-pass" {
		t.Errorf("jobs[1].opts.ChartOpts.Password = %q, want longhorn-pass", got)
	}
}

// --------------------------------------------------------------------------
// dedupeImageJobs tests
// --------------------------------------------------------------------------

// dedupeImageJobs key is (name, platform, excludeExtras); these tests pin
// which of those differences are real pulls and which are duplicates.

func img(name, platform string, excludeExtras bool) imageJob {
	return imageJob{
		img:           v1.Image{Name: name},
		platform:      platform,
		excludeExtras: excludeExtras,
	}
}

func TestDedupeImageJobs(t *testing.T) {
	tests := []struct {
		name string
		in   []imageJob
		want []imageJob
	}{
		{
			name: "no jobs",
			in:   nil,
			want: nil,
		},
		{
			name: "exact duplicates collapse to the first",
			in: []imageJob{
				img("rancher/rancher:v2.9", "linux/amd64", false),
				img("rancher/rancher:v2.9", "linux/amd64", false),
			},
			want: []imageJob{img("rancher/rancher:v2.9", "linux/amd64", false)},
		},
		{
			name: "same name, different platform stays separate",
			in: []imageJob{
				img("rancher/rancher:v2.9", "linux/amd64", false),
				img("rancher/rancher:v2.9", "linux/arm64", false),
			},
			want: []imageJob{
				img("rancher/rancher:v2.9", "linux/amd64", false),
				img("rancher/rancher:v2.9", "linux/arm64", false),
			},
		},
		{
			name: "same name, different excludeExtras stays separate",
			in: []imageJob{
				img("rancher/rancher:v2.9", "linux/amd64", false),
				img("rancher/rancher:v2.9", "linux/amd64", true),
			},
			want: []imageJob{
				img("rancher/rancher:v2.9", "linux/amd64", false),
				img("rancher/rancher:v2.9", "linux/amd64", true),
			},
		},
		{
			name: "different names are never merged",
			in: []imageJob{
				img("rancher/rancher:v2.9", "", false),
				img("rancher/rancher-agent:v2.9", "", false),
			},
			want: []imageJob{
				img("rancher/rancher:v2.9", "", false),
				img("rancher/rancher-agent:v2.9", "", false),
			},
		},
		{
			name: "same repository, different tag stays separate",
			in: []imageJob{
				img("rancher/rancher:v2.9", "", false),
				img("rancher/rancher:v2.10", "", false),
			},
			want: []imageJob{
				img("rancher/rancher:v2.9", "", false),
				img("rancher/rancher:v2.10", "", false),
			},
		},
		{
			name: "first-seen order survives interleaved duplicates",
			in: []imageJob{
				img("c:1", "", false),
				img("a:1", "", false),
				img("c:1", "", false),
				img("b:1", "", false),
				img("a:1", "", false),
			},
			want: []imageJob{
				img("c:1", "", false),
				img("a:1", "", false),
				img("b:1", "", false),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeImageJobs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d jobs, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i].img.Name != tc.want[i].img.Name ||
					got[i].platform != tc.want[i].platform ||
					got[i].excludeExtras != tc.want[i].excludeExtras {
					t.Errorf("job[%d] = (%q, %q, %v), want (%q, %q, %v)",
						i, got[i].img.Name, got[i].platform, got[i].excludeExtras,
						tc.want[i].img.Name, tc.want[i].platform, tc.want[i].excludeExtras)
				}
			}
		})
	}
}

// TestDedupeImageJobs_KeepsFirstOccurrenceFields pins "first occurrence wins"
// on the whole job, not just its key: a later duplicate's other fields (here,
// rewrite) must not overwrite the retained job's.
func TestDedupeImageJobs_KeepsFirstOccurrenceFields(t *testing.T) {
	first := img("rancher/rancher:v2.9", "linux/amd64", false)
	first.rewrite = "mirror/rancher"

	second := img("rancher/rancher:v2.9", "linux/amd64", false)
	second.rewrite = "other/rancher"

	got := dedupeImageJobs([]imageJob{first, second})
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}
	if got[0].rewrite != "mirror/rancher" {
		t.Errorf("rewrite = %q, want mirror/rancher", got[0].rewrite)
	}
}

// TestDedupeImageJobs_DoesNotMutateInput pins that callers keep a usable
// slice: deduping returns a new one rather than compacting in place.
func TestDedupeImageJobs_DoesNotMutateInput(t *testing.T) {
	in := []imageJob{
		img("a:1", "", false),
		img("b:1", "", false),
		img("a:1", "", false),
	}

	_ = dedupeImageJobs(in)

	if len(in) != 3 {
		t.Fatalf("input length changed to %d", len(in))
	}
	for i, want := range []string{"a:1", "b:1", "a:1"} {
		if in[i].img.Name != want {
			t.Errorf("in[%d] = %q, want %q", i, in[i].img.Name, want)
		}
	}
}

// --------------------------------------------------------------------------
// traverseChartLevels graph tests
//
// These cover the traversal in isolation: the seen-set cycle guard, the
// maxChartDepth cap, per-level dependency deduplication, and error
// propagation. Every test injects a chartFetcher, so none of them touch helm,
// the store, or the network.
// --------------------------------------------------------------------------

// newChartJob builds a top-level chartJob for a bare chart name, with the
// per-job *action.ChartPathOptions allocation resolveChartJobs guarantees.
func newChartJob(name string) chartJob {
	return chartJob{
		cfg:  v1.Chart{Name: name},
		opts: flags.AddChartOpts{ChartOpts: &action.ChartPathOptions{}},
	}
}

// recordingFetcher is a chartFetcher that records the name of every chart it
// is asked to fetch and returns the dependencies graph[name] declares.
type recordingFetcher struct {
	mu    sync.Mutex
	seen  []string
	graph map[string][]string
}

func (r *recordingFetcher) fetch(_ context.Context, j chartJob) ([]imageJob, []chartJob, error) {
	r.mu.Lock()
	r.seen = append(r.seen, j.cfg.Name)
	deps := r.graph[j.cfg.Name]
	r.mu.Unlock()

	out := make([]chartJob, 0, len(deps))
	for _, d := range deps {
		out = append(out, newChartJob(d))
	}
	return nil, out, nil
}

func (r *recordingFetcher) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.seen {
		if s == name {
			n++
		}
	}
	return n
}

// TestTraverseChartLevels_TerminatesOnCycle pins the seen-set cycle guard,
// without which the walk recurses forever: A depends on B, B depends back on
// A, and each must be fetched exactly once.
func TestTraverseChartLevels_TerminatesOnCycle(t *testing.T) {
	f := &recordingFetcher{graph: map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}}

	_, charts, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("a")}, 2, f.fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}

	if got := f.count("a"); got != 1 {
		t.Errorf("chart a fetched %d times, want exactly 1", got)
	}
	if got := f.count("b"); got != 1 {
		t.Errorf("chart b fetched %d times, want exactly 1", got)
	}
	if charts != 2 {
		t.Errorf("charts fetched = %d, want 2", charts)
	}
}

// TestTraverseChartLevels_SelfCycleTerminates covers the degenerate cycle a
// chart declaring itself as a dependency would produce.
func TestTraverseChartLevels_SelfCycleTerminates(t *testing.T) {
	f := &recordingFetcher{graph: map[string][]string{"a": {"a"}}}

	_, charts, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("a")}, 2, f.fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}
	if charts != 1 {
		t.Errorf("charts fetched = %d, want 1", charts)
	}
}

// TestTraverseChartLevels_SeenKeyIncludesRepoAndVersion asserts the seen set
// is keyed on name|repoURL|version rather than name alone: the same chart
// name at two versions is two genuinely different pulls.
func TestTraverseChartLevels_SeenKeyIncludesRepoAndVersion(t *testing.T) {
	var mu sync.Mutex
	var fetched []string

	fetch := func(_ context.Context, j chartJob) ([]imageJob, []chartJob, error) {
		mu.Lock()
		fetched = append(fetched, j.cfg.Name+"@"+j.cfg.Version)
		mu.Unlock()

		if j.cfg.Version != "" {
			return nil, nil, nil
		}
		return nil, []chartJob{
			{cfg: v1.Chart{Name: "dep", RepoURL: "https://charts.example.com", Version: "1.0.0"}, opts: flags.AddChartOpts{ChartOpts: &action.ChartPathOptions{}}},
			{cfg: v1.Chart{Name: "dep", RepoURL: "https://charts.example.com", Version: "2.0.0"}, opts: flags.AddChartOpts{ChartOpts: &action.ChartPathOptions{}}},
		}, nil
	}

	_, charts, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("parent")}, 2, fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}
	if charts != 3 {
		t.Fatalf("charts fetched = %d, want 3 (parent + dep@1.0.0 + dep@2.0.0), got %v", charts, fetched)
	}
}

// TestTraverseChartLevels_DedupesRepeatedDependenciesInOneLevel covers two
// siblings declaring the identical dependency: it is fetched once, not twice.
func TestTraverseChartLevels_DedupesRepeatedDependenciesInOneLevel(t *testing.T) {
	f := &recordingFetcher{graph: map[string][]string{
		"a":      {"shared"},
		"b":      {"shared"},
		"shared": nil,
	}}

	_, charts, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("a"), newChartJob("b")}, 2, f.fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}
	if got := f.count("shared"); got != 1 {
		t.Errorf("shared dependency fetched %d times, want exactly 1", got)
	}
	if charts != 3 {
		t.Errorf("charts fetched = %d, want 3", charts)
	}
}

// TestTraverseChartLevels_DepthCapHolds pins maxChartDepth independently of
// the seen set: an infinitely deep chain of distinct charts (which the seen
// set can never stop) must terminate at exactly maxChartDepth levels.
func TestTraverseChartLevels_DepthCapHolds(t *testing.T) {
	var mu sync.Mutex
	n := 0

	fetch := func(_ context.Context, j chartJob) ([]imageJob, []chartJob, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return nil, []chartJob{newChartJob(j.cfg.Name + "-child")}, nil
	}

	_, charts, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("root")}, 2, fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if n != maxChartDepth {
		t.Errorf("fetch called %d times, want exactly maxChartDepth (%d) -- one chart per level on an unbounded chain", n, maxChartDepth)
	}
	if charts != maxChartDepth {
		t.Errorf("charts fetched = %d, want %d", charts, maxChartDepth)
	}
}

// TestTraverseChartLevels_CollectsImagesAcrossLevels asserts images
// discovered at every depth reach the caller, not just the top level's.
func TestTraverseChartLevels_CollectsImagesAcrossLevels(t *testing.T) {
	fetch := func(_ context.Context, j chartJob) ([]imageJob, []chartJob, error) {
		imgs := []imageJob{{img: v1.Image{Name: j.cfg.Name + "-image"}}}
		if j.cfg.Name == "parent" {
			return imgs, []chartJob{newChartJob("child")}, nil
		}
		return imgs, nil, nil
	}

	images, charts, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("parent")}, 2, fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}
	if charts != 2 {
		t.Fatalf("charts fetched = %d, want 2", charts)
	}

	got := map[string]bool{}
	for _, i := range images {
		got[i.img.Name] = true
	}
	for _, want := range []string{"parent-image", "child-image"} {
		if !got[want] {
			t.Errorf("image %q missing from discovered set %v", want, got)
		}
	}
}

// TestTraverseChartLevels_PropagatesFetchError asserts a fetch failure
// surfaces verbatim rather than as an errgroup cancellation, matching
// runImageJobs' semantics.
func TestTraverseChartLevels_PropagatesFetchError(t *testing.T) {
	sentinel := errors.New("chart repo unreachable")

	fetch := func(_ context.Context, j chartJob) ([]imageJob, []chartJob, error) {
		if j.cfg.Name == "bad" {
			return nil, nil, sentinel
		}
		return nil, nil, nil
	}

	_, _, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("bad")}, 1, fetch)
	if !errors.Is(err, sentinel) {
		t.Fatalf("traverseChartLevels error = %v, want it to wrap %v", err, sentinel)
	}
}

// TestTraverseChartLevels_ErrorInDeeperLevelStopsTraversal asserts a failure
// discovered at depth 1 aborts before any depth-2 chart is fetched.
func TestTraverseChartLevels_ErrorInDeeperLevelStopsTraversal(t *testing.T) {
	sentinel := errors.New("subchart missing")
	f := &recordingFetcher{graph: map[string][]string{
		"root": {"mid"},
		"mid":  {"leaf"},
	}}

	fetch := func(ctx context.Context, j chartJob) ([]imageJob, []chartJob, error) {
		if j.cfg.Name == "mid" {
			return nil, nil, sentinel
		}
		return f.fetch(ctx, j)
	}

	if _, _, err := traverseChartLevels(context.Background(), []chartJob{newChartJob("root")}, 1, fetch); !errors.Is(err, sentinel) {
		t.Fatalf("traverseChartLevels error = %v, want it to wrap %v", err, sentinel)
	}
	if got := f.count("leaf"); got != 0 {
		t.Errorf("leaf fetched %d times after its parent level failed, want 0", got)
	}
}

// TestTraverseChartLevels_NoJobs covers the empty-input path.
func TestTraverseChartLevels_NoJobs(t *testing.T) {
	fetch := func(context.Context, chartJob) ([]imageJob, []chartJob, error) {
		return nil, nil, fmt.Errorf("fetch must not be called with no jobs")
	}

	images, charts, err := traverseChartLevels(context.Background(), nil, 4, fetch)
	if err != nil {
		t.Fatalf("traverseChartLevels: %v", err)
	}
	if len(images) != 0 || charts != 0 {
		t.Errorf("images = %v, charts = %d, want none", images, charts)
	}
}

// --------------------------------------------------------------------------
// Concurrent chart pipeline tests
//
// These cover that traverseChartLevels honors its concurrency bound, that
// dependency-derived jobs never share an *action.ChartPathOptions with their
// parent, and that a file:// dependency still resolves after its parent's
// level has completed -- the regression that runChartJobs' single shared temp
// root exists to prevent.
// --------------------------------------------------------------------------

// newFileDependencyChartJob builds a top-level chartJob for
// testdata/chart-with-file-dependency-chart-1.0.0.tgz, whose Chart.yaml
// declares two dependencies that resolve inside the parent's own expanded
// directory (one via file://, one via an empty repository field).
func newFileDependencyChartJob() chartJob {
	return chartJob{
		cfg: v1.Chart{Name: "chart-with-file-dependency-chart-1.0.0.tgz", RepoURL: chartTestdataDir},
		opts: flags.AddChartOpts{
			ChartOpts:       &action.ChartPathOptions{RepoURL: chartTestdataDir},
			AddDependencies: true,
		},
	}
}

// TestTraverseChartLevels_BoundedFanOut asserts the observed peak of
// simultaneously in-flight fetches never exceeds the requested concurrency,
// at every level of the walk.
func TestTraverseChartLevels_BoundedFanOut(t *testing.T) {
	const perLevel = 12

	for _, concurrency := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			var (
				mu       sync.Mutex
				inFlight int
				peak     int
			)

			fetch := func(_ context.Context, j chartJob) ([]imageJob, []chartJob, error) {
				mu.Lock()
				inFlight++
				if inFlight > peak {
					peak = inFlight
				}
				mu.Unlock()

				// Long enough that a broken bound would overlap observably;
				// short enough to keep the whole table under a second.
				time.Sleep(2 * time.Millisecond)

				mu.Lock()
				inFlight--
				mu.Unlock()

				if strings.HasSuffix(j.cfg.Name, "-child") {
					return nil, nil, nil
				}
				return nil, []chartJob{newChartJob(j.cfg.Name + "-child")}, nil
			}

			jobs := make([]chartJob, 0, perLevel)
			for i := 0; i < perLevel; i++ {
				jobs = append(jobs, newChartJob(fmt.Sprintf("chart%d", i)))
			}

			_, charts, err := traverseChartLevels(context.Background(), jobs, concurrency, fetch)
			if err != nil {
				t.Fatalf("traverseChartLevels: %v", err)
			}
			if charts != 2*perLevel {
				t.Errorf("charts fetched = %d, want %d", charts, 2*perLevel)
			}

			mu.Lock()
			defer mu.Unlock()
			if peak > concurrency {
				t.Errorf("peak in-flight fetches = %d, want <= %d", peak, concurrency)
			}
			if peak < 1 {
				t.Errorf("peak in-flight fetches = %d, want at least 1", peak)
			}
		})
	}
}

// TestFetchChart_DependencyJobsDoNotShareChartOpts is the pointer-identity
// regression test for the derived-job path. resolveChartJobs' equivalent test
// covers only top-level jobs; a `depJob := parentJob` struct copy would pass
// that one while still sharing the *action.ChartPathOptions pointee here,
// which under concurrency is a live data race on RepoURL/Version.
func TestFetchChart_DependencyJobsDoNotShareChartOpts(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	parent := newFileDependencyChartJob()
	_, deps, err := fetchChart(ctx, s, parent, t.TempDir(), rso, ro)
	if err != nil {
		t.Fatalf("fetchChart: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("dependency jobs = %d, want 2 (child + crds)", len(deps))
	}

	for i, d := range deps {
		if d.opts.ChartOpts == parent.opts.ChartOpts {
			t.Fatalf("deps[%d] shares its parent's *action.ChartPathOptions pointee", i)
		}
	}
	if deps[0].opts.ChartOpts == deps[1].opts.ChartOpts {
		t.Fatal("deps[0] and deps[1] share one *action.ChartPathOptions pointee")
	}

	parentRepoURL := parent.opts.ChartOpts.RepoURL
	deps[0].opts.ChartOpts.RepoURL = "https://mutated.example.com"
	deps[0].opts.ChartOpts.Version = "0.0.0-mutated"

	if got := parent.opts.ChartOpts.RepoURL; got != parentRepoURL {
		t.Errorf("parent RepoURL = %q after mutating a dependency's, want %q", got, parentRepoURL)
	}
	if got := deps[1].opts.ChartOpts.RepoURL; got == "https://mutated.example.com" {
		t.Error("mutating deps[0] was visible through deps[1]")
	}

	parent.opts.ChartOpts.Version = "9.9.9-mutated"
	for i, d := range deps {
		if d.opts.ChartOpts.Version == "9.9.9-mutated" {
			t.Errorf("mutating the parent was visible through deps[%d]", i)
		}
	}
}

// TestFetchChart_DerivedDependencyFields pins the shape of a derived job:
// dependencies are walked further, images are not rediscovered per subchart,
// no rewrite is inherited, and parent/depth are set for attribution and the
// depth cap.
func TestFetchChart_DerivedDependencyFields(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	parent := newFileDependencyChartJob()
	parent.opts.AddImages = true
	parent.rewrite = "myorg/parent-chart"

	_, deps, err := fetchChart(ctx, s, parent, t.TempDir(), rso, ro)
	if err != nil {
		t.Fatalf("fetchChart: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("dependency jobs = %d, want 2", len(deps))
	}

	for i, d := range deps {
		if !d.opts.AddDependencies {
			t.Errorf("deps[%d].opts.AddDependencies = false, want true", i)
		}
		if d.opts.AddImages {
			t.Errorf("deps[%d].opts.AddImages = true, want false: the parent's render already covered the whole tree", i)
		}
		if d.rewrite != "" {
			t.Errorf("deps[%d].rewrite = %q, want empty", i, d.rewrite)
		}
		if d.depth != parent.depth+1 {
			t.Errorf("deps[%d].depth = %d, want %d", i, d.depth, parent.depth+1)
		}
		if !strings.Contains(d.parent, "chart-with-file-dependency-chart") {
			t.Errorf("deps[%d].parent = %q, want it to name the parent chart's ref", i, d.parent)
		}
	}
}

// TestRunChartJobs_FileDependencyResolvesAfterParentLevel is the
// shared-temp-root regression test. A file:// dependency's chartJob names a
// path *inside* its parent's expanded directory, and BFS fetches it only
// after the parent's whole level has returned. A per-chart temp dir removed
// when fetchChart returns would delete that path out from under the child, so
// this test fails outright under that design rather than flaking.
func TestRunChartJobs_FileDependencyResolvesAfterParentLevel(t *testing.T) {
	for _, concurrency := range []int{1, 4} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			ctx := newTestContext(t)
			s := newTestStore(t)
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()

			if err := runChartJobs(ctx, s, []chartJob{newFileDependencyChartJob()}, concurrency, rso, ro, nil); err != nil {
				t.Fatalf("runChartJobs: %v", err)
			}

			assertArtifactInStore(t, s, "chart-with-file-dependency-chart:1.0.0")
			assertArtifactInStore(t, s, "child:2.0.0")
			assertArtifactInStore(t, s, "crds:0.0.1")
		})
	}
}

// TestRunChartJobs_RemovesTempRoot asserts the shared temp root does not
// outlive the call: it holds every chart's expansion, so leaking one per
// `store sync` invocation would accumulate on disk.
func TestRunChartJobs_RemovesTempRoot(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	rso.TempOverride = t.TempDir()
	ro := defaultCliOpts()

	if err := runChartJobs(ctx, s, []chartJob{newFileDependencyChartJob()}, 2, rso, ro, nil); err != nil {
		t.Fatalf("runChartJobs: %v", err)
	}

	entries, err := os.ReadDir(rso.TempOverride)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", rso.TempOverride, err)
	}
	for _, e := range entries {
		t.Errorf("temp override still contains %q after runChartJobs returned", filepath.Join(rso.TempOverride, e.Name()))
	}
}

// TestFormatAddedLine_NilStats proves formatAddedLine never prints
// "0 layers" when stats is nil, and produces a sane elapsed-only line.
func TestFormatAddedLine_NilStats(t *testing.T) {
	got := formatAddedLine("example.com/repo:v1", nil, 1500*time.Millisecond)

	if strings.Contains(got, "0 layer") {
		t.Errorf("formatAddedLine with nil stats must never print \"0 layers\", got %q", got)
	}
	if !strings.Contains(got, "example.com/repo:v1") {
		t.Errorf("formatAddedLine must include the ref, got %q", got)
	}
	if !strings.Contains(got, "1.5s") {
		t.Errorf("formatAddedLine must include the elapsed time as %%.1fs, got %q", got)
	}
	if !strings.HasPrefix(got, "✓ added") {
		t.Errorf("formatAddedLine must start with \"✓ added\", got %q", got)
	}
}

// TestFormatAddedLine_ZeroValueStats proves formatAddedLine treats a
// zero-value *store.ImageStats (Layers == 0) the same as nil: elapsed-only,
// never "0 layers".
func TestFormatAddedLine_ZeroValueStats(t *testing.T) {
	stats := &store.ImageStats{}
	got := formatAddedLine("example.com/repo:v1", stats, 2*time.Second)

	if strings.Contains(got, "0 layer") {
		t.Errorf("formatAddedLine with zero-value stats must never print \"0 layers\", got %q", got)
	}
	if !strings.Contains(got, "2.0s") {
		t.Errorf("formatAddedLine must include the elapsed time as %%.1fs, got %q", got)
	}
}

// TestFormatAddedLine_WithStats proves formatAddedLine includes layer
// count (correctly pluralized), human-readable byte size, and elapsed time
// when stats has at least one layer.
func TestFormatAddedLine_WithStats(t *testing.T) {
	tests := []struct {
		name    string
		layers  int64
		bytes   int64
		wantSub string
	}{
		{name: "singular layer", layers: 1, bytes: 100, wantSub: "1 layer,"},
		{name: "plural layers", layers: 3, bytes: 1_500_000, wantSub: "3 layers,"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &store.ImageStats{}
			stats.Layers.Store(tt.layers)
			stats.Bytes.Store(tt.bytes)

			got := formatAddedLine("example.com/repo:v1", stats, 3*time.Second)

			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("formatAddedLine = %q, want substring %q", got, tt.wantSub)
			}
			wantSize := humanize.Bytes(uint64(tt.bytes))
			if !strings.Contains(got, wantSize) {
				t.Errorf("formatAddedLine = %q, want it to contain human size %q", got, wantSize)
			}
			if !strings.Contains(got, "3.0s") {
				t.Errorf("formatAddedLine = %q, want it to contain elapsed \"3.0s\"", got)
			}
		})
	}
}

// TestStoreImage_CAFileAndInsecure exercises the insecureSkipTLSVerify / caFile
// plumbing through storeImage -> AddImage. Note: the in-memory registry runs on
// localhost, which go-containerregistry forces to http, so these cases do NOT
// perform a real TLS handshake — the actual CA trust/reject behavior is covered
// by buildTransport's handshake test in pkg/store. What's verified here is caFile
// error propagation, insecure-over-caFile precedence, and that a valid caFile
// doesn't break the pull.
func TestStoreImage_CAFileAndInsecure(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "tls/repo", "v1", rOpts...)
	ref := host + "/tls/repo:v1"

	const missingCA = "/nonexistent/ca.pem"

	t.Run("bad caFile without insecure returns error and stores nothing", func(t *testing.T) {
		s := newTestStore(t)
		insecure := false
		img := v1.Image{Name: ref, CaFile: missingCA, InsecureSkipTLSVerify: insecure}
		err := storeImage(ctx, s, img, "", false,
			defaultRootOpts(s.Root), defaultCliOpts(), "", "", false)
		if err == nil {
			t.Fatal("expected error from unreadable caFile, got nil")
		}
		if n := countArtifactsInStore(t, s); n != 0 {
			t.Errorf("expected nothing stored on caFile error, got %d", n)
		}
	})

	t.Run("non-PEM caFile without insecure returns error", func(t *testing.T) {
		s := newTestStore(t)
		junk := filepath.Join(t.TempDir(), "junk.pem")
		if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
			t.Fatal(err)
		}
		insecure := false
		img := v1.Image{Name: ref, CaFile: junk, InsecureSkipTLSVerify: insecure}
		err := storeImage(ctx, s, img, "", false,
			defaultRootOpts(s.Root), defaultCliOpts(), "", "", false)
		if err == nil {
			t.Fatal("expected error from non-PEM caFile, got nil")
		}
	})

	t.Run("insecure takes precedence over bad caFile", func(t *testing.T) {
		s := newTestStore(t)
		// insecure=true short-circuits before caFile is read; the bogus path is
		// ignored and the pull still succeeds. If caFile were read first, the
		// pull would error and nothing would be stored.
		insecure := true
		img := v1.Image{Name: ref, CaFile: missingCA, InsecureSkipTLSVerify: insecure}
		err := storeImage(ctx, s, img, "", false,
			defaultRootOpts(s.Root), defaultCliOpts(), "", "", false)
		if err != nil {
			t.Fatalf("insecure should ignore caFile, got: %v", err)
		}
		assertArtifactInStore(t, s, "tls/repo:v1")
	})

	t.Run("valid caFile without insecure is accepted", func(t *testing.T) {
		s := newTestStore(t)
		insecure := false
		img := v1.Image{Name: ref, CaFile: writeCAFile(t), InsecureSkipTLSVerify: insecure}
		err := storeImage(ctx, s, img, "", false,
			defaultRootOpts(s.Root), defaultCliOpts(), "", "", false)
		if err != nil {
			t.Fatalf("valid caFile should be accepted, got: %v", err)
		}
		assertArtifactInStore(t, s, "tls/repo:v1")
	})
}

// TestDigestRefTag covers digestRefTag's recovery of the tag component from a
// "repo:tag@sha256:..." reference's original string, and the cases where
// there is no tag to recover.
func TestDigestRefTag(t *testing.T) {
	const hex = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestSuffix := "@sha256:" + hex

	tests := []struct {
		name    string
		ref     string
		wantOK  bool
		wantTag string // TagStr(), only checked when wantOK
	}{
		{
			name:    "repo with tag and digest recovers the tag",
			ref:     "example.com/repo:v1" + digestSuffix,
			wantOK:  true,
			wantTag: "v1",
		},
		{
			name:   "plain digest ref with no tag has nothing to recover",
			ref:    "example.com/repo" + digestSuffix,
			wantOK: false,
		},
		{
			name:    "registry with port and a tag still recovers the tag",
			ref:     "example.com:5000/repo:v1" + digestSuffix,
			wantOK:  true,
			wantTag: "v1",
		},
		{
			name:   "registry with port and no tag is not mistaken for one",
			ref:    "example.com:5000/repo" + digestSuffix,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := goname.NewDigest(tt.ref)
			if err != nil {
				t.Fatalf("NewDigest(%q): %v", tt.ref, err)
			}

			tag, ok := digestRefTag(d)
			if ok != tt.wantOK {
				t.Fatalf("digestRefTag(%q) ok = %v, want %v", tt.ref, ok, tt.wantOK)
			}
			if ok && tag.TagStr() != tt.wantTag {
				t.Errorf("digestRefTag(%q) tag = %q, want %q", tt.ref, tag.TagStr(), tt.wantTag)
			}
		})
	}
}

// writeCAFile writes a valid self-signed cert PEM to a temp file and returns its
// path. Its only job is to be a parseable CA file (AppendCertsFromPEM succeeds).
func writeCAFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
