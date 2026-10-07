package store

import (
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// --------------------------------------------------------------------------
// Unit tests — formatReference
// --------------------------------------------------------------------------

func TestFormatReference(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		kind string
		want string
	}{
		{
			name: "tag without dash",
			ref:  "rancher/rancher:v2.8.5-dev.hauler/imageIndex",
			kind: "dev.hauler/imageIndex",
			want: "rancher/rancher:v2.8.5 [dev.hauler/imageIndex]",
		},
		{
			name: "tag with dash keeps the whole tag",
			ref:  "hauler-dev/library/nginx:1.25-alpine-dev.hauler/imageIndex",
			kind: "dev.hauler/imageIndex",
			want: "hauler-dev/library/nginx:1.25-alpine [dev.hauler/imageIndex]",
		},
		{
			name: "tag with several dashes",
			ref:  "repo:v1.0.0-rc-1-dev.hauler/image",
			kind: "dev.hauler/image",
			want: "repo:v1.0.0-rc-1 [dev.hauler/image]",
		},
		{
			name: "registry port and cosign sigs kind",
			ref:  "host:5000/repo:1.2-alpine-dev.hauler/sigs",
			kind: "dev.hauler/sigs",
			want: "host:5000/repo:1.2-alpine [dev.hauler/sigs]",
		},
		{
			name: "referrer kind with subject digest",
			ref:  "repo:v1-dev.hauler/referrers/abc123",
			kind: "dev.hauler/referrers/abc123",
			want: "repo:v1 [dev.hauler/referrers/abc123]",
		},
		{
			name: "empty kind returns unchanged",
			ref:  "repo:1.25-alpine",
			kind: "",
			want: "repo:1.25-alpine",
		},
		{
			name: "kind not at the end returns unchanged",
			ref:  "repo:1.25-alpine",
			kind: "dev.hauler/image",
			want: "repo:1.25-alpine",
		},
		{
			name: "only the kind returns unchanged",
			ref:  "-dev.hauler/image",
			kind: "dev.hauler/image",
			want: "-dev.hauler/image",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatReference(tc.ref, tc.kind)
			if got != tc.want {
				t.Errorf("formatReference(%q, %q) = %q, want %q", tc.ref, tc.kind, got, tc.want)
			}
		})
	}
}

// TestArtifactTypeSuffixedKinds confirms artifactType classifies subject-suffixed
// cosign kinds ("dev.hauler/sigs/<hex>", etc, written for a multi-arch child
// manifest so nameMapKey's <ref>-<kind> stays unique) the same as their plain form,
// since it drove `store remove`'s audit-log Type field via an exact-match switch
// that missed the suffix before consts.SigKindExt was adopted here.
func TestArtifactTypeSuffixedKinds(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	cases := []struct {
		kind string
		want string
	}{
		{consts.KindAnnotationSigs + "/0123abcd", "sigs"},
		{consts.KindAnnotationAtts + "/0123abcd", "atts"},
		{consts.KindAnnotationSboms + "/0123abcd", "sbom"},
	}
	for _, c := range cases {
		desc := ocispec.Descriptor{Annotations: map[string]string{consts.KindAnnotationName: c.kind}}
		if got := artifactType(ctx, s, desc); got != c.want {
			t.Errorf("artifactType(kind=%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// --------------------------------------------------------------------------
// Integration tests — RemoveCmd
// --------------------------------------------------------------------------

func TestRemoveCmd_Force(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	url := seedFileInHTTPServer(t, "removeme.txt", "file-to-remove")
	if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	if n := countArtifactsInStore(t, s); n == 0 {
		t.Fatal("expected at least 1 artifact after storeFile, got 0")
	}

	// Confirm the artifact ref contains "removeme".
	var ref string
	if err := s.Walk(func(reference string, _ ocispec.Descriptor) error {
		if strings.Contains(reference, "removeme") {
			ref = reference
		}
		return nil
	}); err != nil {
		t.Fatalf("walk to find ref: %v", err)
	}
	if ref == "" {
		t.Fatal("could not find stored artifact reference containing 'removeme'")
	}

	if err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, "removeme", defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("RemoveCmd: %v", err)
	}

	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after removal, got %d", n)
	}
}

func TestRemoveCmd_NotFound(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, "nonexistent-ref", defaultCliOpts(), defaultRootOpts(s.Root))
	if err == nil {
		t.Fatal("expected error for non-existent ref, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error containing 'not found', got: %v", err)
	}
}

// TestRemoveCmd_ContainerdImageName confirms that a
// registry-prefixed ref (which only appears in the io.containerd.image.name
// annotation, not the registry-stripped org.opencontainers.image.ref.name
// used to key the store's nameMap) still matches for removal.
func TestRemoveCmd_ContainerdImageName(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "test/repo", "v1", rOpts...)

	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/repo:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	if n := countArtifactsInStore(t, s); n == 0 {
		t.Fatal("expected at least 1 artifact after storeImage, got 0")
	}

	// The registry-qualified ref only lives in io.containerd.image.name;
	// org.opencontainers.image.ref.name (and the nameMap key derived from it)
	// only holds the registry-stripped short form "test/repo:v1".
	fullRef := host + "/test/repo:v1"
	if err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, fullRef, ro, rso); err != nil {
		t.Fatalf("RemoveCmd with fully-qualified ref: %v", err)
	}

	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after removal by containerd image name, got %d", n)
	}
}

func TestRemoveCmd_Force_MultipleMatches(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// Seed two file artifacts whose names share the substring "testfile".
	url1 := seedFileInHTTPServer(t, "testfile-alpha.txt", "content-alpha")
	url2 := seedFileInHTTPServer(t, "testfile-beta.txt", "content-beta")

	if err := storeFile(ctx, s, v1.File{Path: url1}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile alpha: %v", err)
	}
	if err := storeFile(ctx, s, v1.File{Path: url2}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile beta: %v", err)
	}

	if n := countArtifactsInStore(t, s); n < 2 {
		t.Fatalf("expected at least 2 artifacts, got %d", n)
	}

	// Remove using a substring that matches both.
	if err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, "testfile", defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("RemoveCmd: %v", err)
	}

	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after removal of both, got %d", n)
	}
}
