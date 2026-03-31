package store

import (
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	helmchart "helm.sh/helm/v3/pkg/chart"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/consts"
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
// top-level testdata directory, matching the convention in chart_test.go.
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

func TestRewriteReference(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("valid rewrite updates store annotations", func(t *testing.T) {
		host, rOpts := newTestRegistry(t)
		seedImage(t, host, "src/repo", "v1", rOpts...)

		s := newTestStore(t)
		if err := s.AddImage(ctx, host+"/src/repo:v1", "", false, rOpts...); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		oldRef, err := name.NewTag(host+"/src/repo:v1", name.Insecure)
		if err != nil {
			t.Fatalf("parse oldRef: %v", err)
		}
		newRef, err := name.NewTag(host+"/dst/repo:v2", name.Insecure)
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
		oldRef, _ := name.NewTag("docker.io/missing/repo:v1")
		newRef, _ := name.NewTag("docker.io/new/repo:v2")
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

		oldRef, _ := name.NewTag("nginx:latest") // → index.docker.io/library/nginx:latest
		newRef, _ := name.NewTag("nginx:v2")     // → index.docker.io/library/nginx:v2
		rawRewrite := "nginx:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// library/ must be stripped; registry stays index.docker.io
		assertAnnotationsInStore(t, s, "nginx:v2", "index.docker.io/nginx:v2")
	})

	t.Run("explicit docker.io rewrite preserves library/ prefix", func(t *testing.T) {
		s := newTestStore(t)
		seedStoreDescriptor(t, s, map[string]string{
			ocispec.AnnotationRefName:     "library/nginx:latest",
			consts.ContainerdImageNameKey: "index.docker.io/library/nginx:latest",
		})

		oldRef, _ := name.NewTag("nginx:latest")
		newRef, _ := name.NewTag("docker.io/nginx:v2") // → index.docker.io/library/nginx:v2
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

		oldRef, _ := name.NewTag("nginx:latest")
		newRef, _ := name.NewTag("index.docker.io/nginx:v2") // → index.docker.io/library/nginx:v2
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
		if err := s.AddImage(ctx, host+"/src/repo:v1", "", false, rOpts...); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		oldRef, _ := name.NewTag(host+"/src/repo:v1", name.Insecure)
		newRef, _ := name.NewTag("newrepo/img:v2") // defaults to index.docker.io
		rawRewrite := "newrepo/img:v2"

		if err := rewriteReference(ctx, s, oldRef, newRef, rawRewrite); err != nil {
			t.Fatalf("rewriteReference: %v", err)
		}
		// condition fires → registry reverts to host, no library/ to strip
		assertAnnotationsInStore(t, s, "newrepo/img:v2", host+"/newrepo/img:v2")
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
		if err := storeFile(ctx, s, v1.File{Path: tmp.Name()}); err != nil {
			t.Fatalf("storeFile: %v", err)
		}
		assertArtifactInStore(t, s, filepath.Base(tmp.Name()))
	})

	t.Run("HTTP URL stored under basename", func(t *testing.T) {
		url := seedFileInHTTPServer(t, "script.sh", "#!/bin/sh\necho ok")
		s := newTestStore(t)
		if err := storeFile(ctx, s, v1.File{Path: url}); err != nil {
			t.Fatalf("storeFile: %v", err)
		}
		assertArtifactInStore(t, s, "script.sh")
	})

	t.Run("name override changes stored ref", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "orig-*.txt")
		if err != nil {
			t.Fatal(err)
		}
		tmp.Close()

		s := newTestStore(t)
		if err := storeFile(ctx, s, v1.File{Path: tmp.Name(), Name: "custom.sh"}); err != nil {
			t.Fatalf("storeFile: %v", err)
		}
		assertArtifactInStore(t, s, "custom.sh")
	})

	t.Run("nonexistent local path returns error", func(t *testing.T) {
		s := newTestStore(t)
		err := storeFile(ctx, s, v1.File{Path: "/nonexistent/path/missing-file.txt"})
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

	o := &flags.AddFileOpts{Name: "renamed.txt"}
	if err := AddFileCmd(ctx, o, s, tmp.Name()); err != nil {
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

			err := storeImage(ctx, s, v1.Image{Name: tc.imageName}, "", false, rso, ro, "")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantInStore != "" {
				assertArtifactInStore(t, s, tc.wantInStore)
			}
		})
	}
}

func TestStoreImage_Rewrite(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	t.Run("explicit rewrite tag changes ref", func(t *testing.T) {
		seedImage(t, host, "src/repo", "v1", rOpts...)
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		err := storeImage(ctx, s, v1.Image{Name: host + "/src/repo:v1"}, "", false, rso, ro, "newrepo/img:v2")
		if err != nil {
			t.Fatalf("storeImage with rewrite: %v", err)
		}
		assertArtifactInStore(t, s, "newrepo/img:v2")
	})

	t.Run("rewrite without tag inherits source tag", func(t *testing.T) {
		seedImage(t, host, "src/repo", "v3", rOpts...)
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		err := storeImage(ctx, s, v1.Image{Name: host + "/src/repo:v3"}, "", false, rso, ro, "newrepo/img")
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
		err = storeImage(ctx, s, v1.Image{Name: digestRef}, "", false, rso, ro, "newrepo/img")
		if err == nil {
			t.Fatal("expected error for digest ref rewrite without explicit tag, got nil")
		}
		if !strings.Contains(err.Error(), "cannot rewrite digest reference") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestStoreImage_MultiArch(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedIndex(t, host, "test/multiarch", "v1", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/multiarch:v1"}, "", false, rso, ro, ""); err != nil {
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

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/multiarch:v2"}, "linux/amd64", false, rso, ro, ""); err != nil {
		t.Fatalf("storeImage with platform filter: %v", err)
	}
	// Platform filter resolves a single manifest from the index → stored as a single image.
	assertArtifactKindInStore(t, s, "test/multiarch:v2", consts.KindAnnotationImage)
}

func TestStoreImage_CosignV2Artifacts(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)

	img := seedImage(t, host, "test/signed", "v1", rOpts...)
	seedCosignV2Artifacts(t, host, "test/signed", img, rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/signed:v1"}, "", false, rso, ro, ""); err != nil {
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

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/image:v1"}, "", false, rso, ro, ""); err != nil {
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

		if err := storeImage(ctx, s, v1.Image{Name: host + "/test/signed:v1"}, "", true, rso, ro, ""); err != nil {
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

		if err := storeImage(ctx, s, v1.Image{Name: host + "/test/image:v1"}, "", true, rso, ro, ""); err != nil {
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

		if err := storeImage(ctx, s, v1.Image{Name: host + "/test/signed:v2"}, "", false, rso, ro, ""); err != nil {
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
	if err := AddChartCmd(ctx, o, s, "chart-with-file-dependency-chart-1.0.0.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	assertArtifactInStore(t, s, "chart-with-file-dependency-chart")
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
}
