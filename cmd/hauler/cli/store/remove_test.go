package store

import (
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
)

// --------------------------------------------------------------------------
// Unit tests — formatReference
// --------------------------------------------------------------------------

func TestFormatReference(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "empty string returns empty",
			ref:  "",
			want: "",
		},
		{
			name: "no colon returns unchanged",
			ref:  "nocolon",
			want: "nocolon",
		},
		{
			name: "tag without dash returns unchanged",
			ref:  "rancher/rancher:v2.8.5",
			want: "rancher/rancher:v2.8.5",
		},
		{
			name: "cosign sig tag splits at first dash after last colon",
			ref:  "repo:sha256-abc123.sig",
			want: "repo:sha256 [abc123.sig]",
		},
		{
			name: "cosign att tag format",
			ref:  "myrepo:sha256-deadbeef.att",
			want: "myrepo:sha256 [deadbeef.att]",
		},
		{
			name: "cosign sbom tag format",
			ref:  "myrepo:sha256-deadbeef.sbom",
			want: "myrepo:sha256 [deadbeef.sbom]",
		},
		{
			name: "tag is only a dash returns unchanged (empty suffix)",
			ref:  "repo:-",
			want: "repo:-",
		},
		{
			name: "multiple colons uses last one",
			ref:  "host:5000/repo:sha256-abc.sig",
			want: "host:5000/repo:sha256 [abc.sig]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatReference(tc.ref)
			if got != tc.want {
				t.Errorf("formatReference(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Integration tests — RemoveCmd
// --------------------------------------------------------------------------

func TestRemoveCmd_Force(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	url := seedFileInHTTPServer(t, "removeme.txt", "file-to-remove")
	if err := storeFile(ctx, s, v1.File{Path: url}, true); err != nil {
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

	if err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, "removeme"); err != nil {
		t.Fatalf("RemoveCmd: %v", err)
	}

	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after removal, got %d", n)
	}
}

func TestRemoveCmd_NotFound(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, "nonexistent-ref")
	if err == nil {
		t.Fatal("expected error for non-existent ref, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error containing 'not found', got: %v", err)
	}
}

func TestRemoveCmd_Force_MultipleMatches(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// Seed two file artifacts whose names share the substring "testfile".
	url1 := seedFileInHTTPServer(t, "testfile-alpha.txt", "content-alpha")
	url2 := seedFileInHTTPServer(t, "testfile-beta.txt", "content-beta")

	if err := storeFile(ctx, s, v1.File{Path: url1}, true); err != nil {
		t.Fatalf("storeFile alpha: %v", err)
	}
	if err := storeFile(ctx, s, v1.File{Path: url2}, true); err != nil {
		t.Fatalf("storeFile beta: %v", err)
	}

	if n := countArtifactsInStore(t, s); n < 2 {
		t.Fatalf("expected at least 2 artifacts, got %d", n)
	}

	// Remove using a substring that matches both.
	if err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, s, "testfile"); err != nil {
		t.Fatalf("RemoveCmd: %v", err)
	}

	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after removal of both, got %d", n)
	}
}
