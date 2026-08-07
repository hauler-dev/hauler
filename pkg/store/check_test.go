package store_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/store"
)

// newCheckTestRegistry starts an in-memory OCI registry for check tests.
func newCheckTestRegistry(t *testing.T) (host string, opts []remote.Option) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	opts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, opts
}

// newCheckTestStore creates a fresh store.Layout in a temp directory.
func newCheckTestStore(t *testing.T) *store.Layout {
	t.Helper()
	s, err := store.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	return s
}

// pushAndAddImage pushes a random 2-layer image to the test registry under
// host/repo:tag and adds it to s via AddImage, returning the manifest
// descriptor as recorded in the store's index.
func pushAndAddImage(t *testing.T, s *store.Layout, host, repo, tag string, opts []remote.Option) ocispec.Descriptor {
	t.Helper()
	img, err := random.Image(256, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	return pushAndAddExistingImage(t, s, host, repo, tag, img, opts)
}

// pushAndAddExistingImage pushes img (already constructed) to the test registry
// under host/repo:tag and adds it to s via AddImage, returning the manifest
// descriptor as recorded in the store's index.
func pushAndAddExistingImage(t *testing.T, s *store.Layout, host, repo, tag string, img gcrv1.Image, opts []remote.Option) ocispec.Descriptor {
	t.Helper()
	ref, err := gname.NewTag(host+"/"+repo+":"+tag, gname.Insecure)
	if err != nil {
		t.Fatalf("NewTag: %v", err)
	}
	if err := remote.Write(ref, img, opts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}
	if _, err := s.AddImage(context.Background(), ref.Name(), "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	return findManifestDescForRef(t, s, repo+":"+tag)
}

// findManifestDescForRef walks the store's index and returns the descriptor whose
// AnnotationRefName contains refSubstr.
func findManifestDescForRef(t *testing.T, s *store.Layout, refSubstr string) ocispec.Descriptor {
	t.Helper()
	var found ocispec.Descriptor
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstr) {
			found = desc
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if found.Digest == "" {
		t.Fatalf("no manifest found for ref containing %q", refSubstr)
	}
	return found
}

// TestCheck_HealthyStore checks that a freshly-populated store with no
// corruption reports every artifact as OK with no problems.
func TestCheck_HealthyStore(t *testing.T) {
	s := newCheckTestStore(t)
	host, opts := newCheckTestRegistry(t)

	desc1 := pushAndAddImage(t, s, host, "test/img1", "v1", opts)
	desc2 := pushAndAddImage(t, s, host, "test/img2", "v1", opts)

	c := s.NewChecker()
	ctx := context.Background()

	for _, desc := range []ocispec.Descriptor{desc1, desc2} {
		res := c.Check(ctx, desc)
		if !res.OK {
			t.Errorf("expected healthy artifact %s to check OK, got problems: %+v", desc.Digest, res.Problems)
		}
		if len(res.Problems) != 0 {
			t.Errorf("expected no problems for %s, got %+v", desc.Digest, res.Problems)
		}
	}
}

// TestCheck_MissingLayerBlob checks that a deleted layer blob file is
// reported as BlobMissing, naming the correct digest.
func TestCheck_MissingLayerBlob(t *testing.T) {
	s := newCheckTestStore(t)
	host, opts := newCheckTestRegistry(t)

	desc := pushAndAddImage(t, s, host, "test/missing", "v1", opts)
	manifest := readManifestBlob(t, s.Root, desc.Digest)
	if len(manifest.Layers) == 0 {
		t.Fatal("expected at least one layer in test image")
	}
	victim := manifest.Layers[0]

	if err := os.Remove(blobPath(s.Root, victim.Digest)); err != nil {
		t.Fatalf("remove layer blob: %v", err)
	}

	c := s.NewChecker()
	res := c.Check(context.Background(), desc)
	if res.OK {
		t.Fatal("expected check to report a problem after deleting a layer blob")
	}

	var found *store.BlobResult
	for i := range res.Problems {
		if res.Problems[i].Digest == victim.Digest.String() {
			found = &res.Problems[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a problem for digest %s, got %+v", victim.Digest, res.Problems)
	}
	if found.Status != store.BlobMissing {
		t.Errorf("status = %q, want %q", found.Status, store.BlobMissing)
	}
}

// TestCheckBlob_SizeMismatchShortCircuits proves that CheckBlob detects a
// truncated (shorter) blob via its size alone, without ever hashing the
// content: HashCount for the digest must remain 0.
func TestCheckBlob_SizeMismatchShortCircuits(t *testing.T) {
	s := newCheckTestStore(t)
	host, opts := newCheckTestRegistry(t)

	desc := pushAndAddImage(t, s, host, "test/truncated", "v1", opts)
	manifest := readManifestBlob(t, s.Root, desc.Digest)
	victim := manifest.Layers[0]
	path := blobPath(s.Root, victim.Digest)

	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original blob: %v", err)
	}
	half := orig[:len(orig)/2]
	if err := os.WriteFile(path, half, 0o644); err != nil {
		t.Fatalf("truncate blob: %v", err)
	}

	c := s.NewChecker()
	res := c.CheckBlob(ocispec.Descriptor{Digest: victim.Digest, Size: victim.Size})

	if res.Status != store.BlobSizeMismatch {
		t.Fatalf("status = %q, want %q (detail: %s)", res.Status, store.BlobSizeMismatch, res.Detail)
	}
	wantDetail := fmt.Sprintf("expected %d bytes, found %d", victim.Size, len(half))
	if res.Detail != wantDetail {
		t.Errorf("detail = %q, want %q", res.Detail, wantDetail)
	}

	// Prove the check never reached the hashing step: a size mismatch is
	// detected via os.Stat alone, before the file's content is ever streamed
	// through the digest verifier.
	if got := c.HashCount(victim.Digest.String()); got != 0 {
		t.Errorf("HashCount = %d, want 0 (size mismatch must short-circuit before hashing)", got)
	}
}

// TestCheckBlob_SameLengthCorruptionIsDigestMismatch is the most important test
// in this file: it proves the checker performs real content hashing rather than
// merely stat-ing the file. A same-length, in-place byte flip preserves file size,
// so only a genuine digest check (not a size check) can catch it.
func TestCheckBlob_SameLengthCorruptionIsDigestMismatch(t *testing.T) {
	s := newCheckTestStore(t)
	host, opts := newCheckTestRegistry(t)

	desc := pushAndAddImage(t, s, host, "test/corrupt", "v1", opts)
	manifest := readManifestBlob(t, s.Root, desc.Digest)
	victim := manifest.Layers[0]
	path := blobPath(s.Root, victim.Digest)

	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original blob: %v", err)
	}
	corrupted := append([]byte(nil), orig...)
	// Flip a byte at a fixed offset, preserving the exact file length.
	corrupted[0] ^= 0xFF
	if err := os.WriteFile(path, corrupted, 0o644); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}
	if len(corrupted) != len(orig) {
		t.Fatalf("test setup bug: corrupted length %d != original length %d", len(corrupted), len(orig))
	}

	c := s.NewChecker()
	res := c.CheckBlob(ocispec.Descriptor{Digest: victim.Digest, Size: victim.Size})

	if res.Status != store.BlobDigestMismatch {
		t.Fatalf("status = %q, want %q (detail: %s) -- a size-only check would incorrectly pass this same-length corruption",
			res.Status, store.BlobDigestMismatch, res.Detail)
	}

	// This corruption necessarily requires the content to have actually been hashed.
	if got := c.HashCount(victim.Digest.String()); got != 1 {
		t.Errorf("HashCount = %d, want 1", got)
	}
}

// TestCheck_CorruptManifestStopsDescending checks that when the manifest
// blob itself fails its digest check, Check reports exactly that one
// problem and does not attempt to check (or hash) the config/layer blobs it
// references, since a manifest that fails its own digest check cannot be
// trusted to accurately name its children.
func TestCheck_CorruptManifestStopsDescending(t *testing.T) {
	s := newCheckTestStore(t)
	host, opts := newCheckTestRegistry(t)

	desc := pushAndAddImage(t, s, host, "test/badmanifest", "v1", opts)
	manifest := readManifestBlob(t, s.Root, desc.Digest)
	if len(manifest.Layers) == 0 {
		t.Fatal("expected at least one layer")
	}

	manifestPath := blobPath(s.Root, desc.Digest)
	orig, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest blob: %v", err)
	}
	corrupted := append([]byte(nil), orig...)
	corrupted[0] ^= 0xFF
	if err := os.WriteFile(manifestPath, corrupted, 0o644); err != nil {
		t.Fatalf("corrupt manifest blob: %v", err)
	}
	if len(corrupted) != len(orig) {
		t.Fatalf("test setup bug: corrupted length %d != original length %d", len(corrupted), len(orig))
	}

	c := s.NewChecker()
	res := c.Check(context.Background(), desc)

	if res.OK {
		t.Fatal("expected check to fail for a corrupted manifest blob")
	}
	if len(res.Problems) != 1 {
		t.Fatalf("expected exactly one problem (the manifest itself), got %d: %+v", len(res.Problems), res.Problems)
	}
	if res.Problems[0].Digest != desc.Digest.String() {
		t.Errorf("problem digest = %q, want %q (the manifest itself)", res.Problems[0].Digest, desc.Digest)
	}
	if res.Problems[0].Status != store.BlobDigestMismatch {
		t.Errorf("problem status = %q, want %q", res.Problems[0].Status, store.BlobDigestMismatch)
	}

	// Prove recursion truly stopped: none of the config/layer digests the
	// (untrustworthy) manifest names were ever hashed.
	if got := c.HashCount(manifest.Config.Digest.String()); got != 0 {
		t.Errorf("config HashCount = %d, want 0 (must not descend into a manifest that failed its own check)", got)
	}
	for _, l := range manifest.Layers {
		if got := c.HashCount(l.Digest.String()); got != 0 {
			t.Errorf("layer %s HashCount = %d, want 0", l.Digest, got)
		}
	}
}

// TestCheck_SharedLayerMemoizedAcrossImages checks that when two images
// share a common layer, corrupting that shared blob is reported by both
// images' Check results, and that the shared blob is only ever hashed once
// across the whole run (proven via the Checker's per-digest memo).
func TestCheck_SharedLayerMemoizedAcrossImages(t *testing.T) {
	s := newCheckTestStore(t)
	host, opts := newCheckTestRegistry(t)

	sharedData := []byte(strings.Repeat("shared-layer-content", 100))
	sharedLayer := static.NewLayer(sharedData, gvtypes.OCILayer)

	uniqueLayer1, err := random.Layer(128, gvtypes.OCILayer)
	if err != nil {
		t.Fatalf("random.Layer 1: %v", err)
	}
	uniqueLayer2, err := random.Layer(128, gvtypes.OCILayer)
	if err != nil {
		t.Fatalf("random.Layer 2: %v", err)
	}

	img1, err := mutate.AppendLayers(empty.Image, sharedLayer, uniqueLayer1)
	if err != nil {
		t.Fatalf("build img1: %v", err)
	}
	img2, err := mutate.AppendLayers(empty.Image, sharedLayer, uniqueLayer2)
	if err != nil {
		t.Fatalf("build img2: %v", err)
	}

	desc1 := pushAndAddExistingImage(t, s, host, "test/shared1", "v1", img1, opts)
	desc2 := pushAndAddExistingImage(t, s, host, "test/shared2", "v1", img2, opts)

	sharedDigest, err := sharedLayer.Digest()
	if err != nil {
		t.Fatalf("shared layer digest: %v", err)
	}

	// Corrupt the shared blob in place, preserving its length.
	path := blobPath(s.Root, digest.Digest(sharedDigest.String()))
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared blob: %v", err)
	}
	corrupted := append([]byte(nil), orig...)
	corrupted[0] ^= 0xFF
	if err := os.WriteFile(path, corrupted, 0o644); err != nil {
		t.Fatalf("corrupt shared blob: %v", err)
	}

	c := s.NewChecker()
	ctx := context.Background()

	res1 := c.Check(ctx, desc1)
	res2 := c.Check(ctx, desc2)

	for name, res := range map[string]store.CheckResult{"img1": res1, "img2": res2} {
		if res.OK {
			t.Errorf("%s: expected check to report the shared blob corruption", name)
		}
		found := false
		for _, p := range res.Problems {
			if p.Digest == sharedDigest.String() {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected a problem for shared digest %s, got %+v", name, sharedDigest, res.Problems)
		}
	}

	if got := c.HashCount(sharedDigest.String()); got != 1 {
		t.Errorf("shared layer HashCount = %d, want 1 (memoization must prevent re-hashing across images)", got)
	}
}
