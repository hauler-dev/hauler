package content

// oci_test.go covers the annotation-normalization correctness of LoadIndex()
// and ociPusher.Push(). Specifically, it verifies that descriptors returned
// by Walk() carry the normalized dev.hauler/... kind annotation value, not the
// legacy dev.cosignproject.cosign/... value that may be present on disk.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/pkg/consts"
)

// buildMinimalOCILayout writes the smallest valid OCI layout (oci-layout marker
// + index.json with the supplied descriptors) into dir. No blobs are written;
// this is sufficient for testing LoadIndex/Walk without a full store.
func buildMinimalOCILayout(t *testing.T, dir string, manifests []ocispec.Descriptor) {
	t.Helper()

	// oci-layout marker
	layoutMarker := map[string]string{"imageLayoutVersion": "1.0.0"}
	markerData, err := json.Marshal(layoutMarker)
	if err != nil {
		t.Fatalf("marshal oci-layout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ocispec.ImageLayoutFile), markerData, 0644); err != nil {
		t.Fatalf("write oci-layout: %v", err)
	}

	// index.json
	idx := ocispec.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: manifests,
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal index.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), data, 0644); err != nil {
		t.Fatalf("write index.json: %v", err)
	}
}

// fakeDigest returns a syntactically valid digest string that can be used in
// test descriptors without any real blob.
func fakeDigest(hex string) digest.Digest {
	// pad hex to 64 chars
	for len(hex) < 64 {
		hex += "0"
	}
	return digest.Digest("sha256:" + hex)
}

// --------------------------------------------------------------------------
// TestLoadIndex_NormalizesLegacyKindInDescriptorAnnotations
// --------------------------------------------------------------------------

// TestLoadIndex_NormalizesLegacyKindInDescriptorAnnotations verifies that
// after LoadIndex() (called implicitly by Walk()), every descriptor returned
// by Walk carries a normalized dev.hauler/... kind annotation, not the legacy
// dev.cosignproject.cosign/... value stored on disk.
func TestLoadIndex_NormalizesLegacyKindInDescriptorAnnotations(t *testing.T) {
	dir := t.TempDir()

	legacyKinds := []string{
		"dev.cosignproject.cosign/image",
		"dev.cosignproject.cosign/imageIndex",
		"dev.cosignproject.cosign/sigs",
		"dev.cosignproject.cosign/atts",
		"dev.cosignproject.cosign/sboms",
	}

	var manifests []ocispec.Descriptor
	for i, legacyKind := range legacyKinds {
		d := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    fakeDigest(strings.Repeat(string(rune('a'+i)), 1)),
			Size:      100,
			Annotations: map[string]string{
				ocispec.AnnotationRefName: "example.com/repo:tag" + strings.Repeat(string(rune('a'+i)), 1),
				consts.KindAnnotationName: legacyKind,
			},
		}
		manifests = append(manifests, d)
	}

	buildMinimalOCILayout(t, dir, manifests)

	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	var walked []ocispec.Descriptor
	if err := o.Walk(func(_ string, desc ocispec.Descriptor) error {
		walked = append(walked, desc)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(walked) == 0 {
		t.Fatal("Walk returned no descriptors")
	}

	const legacyPrefix = "dev.cosignproject.cosign"
	const newPrefix = "dev.hauler"
	for _, desc := range walked {
		kind := desc.Annotations[consts.KindAnnotationName]
		if strings.HasPrefix(kind, legacyPrefix) {
			t.Errorf("descriptor %s: Walk returned legacy kind %q; want normalized dev.hauler/... value",
				desc.Digest, kind)
		}
		if !strings.HasPrefix(kind, newPrefix) {
			t.Errorf("descriptor %s: Walk returned unexpected kind %q; want dev.hauler/... prefix",
				desc.Digest, kind)
		}
	}
}

// --------------------------------------------------------------------------
// TestLoadIndex_DoesNotMutateOnDiskAnnotations
// --------------------------------------------------------------------------

// TestLoadIndex_DoesNotMutateOnDiskAnnotations verifies that the normalization
// performed by LoadIndex() is in-memory only: the index.json on disk must
// still carry the original (legacy) annotation values after a Walk() call.
func TestLoadIndex_DoesNotMutateOnDiskAnnotations(t *testing.T) {
	dir := t.TempDir()

	legacyKind := "dev.cosignproject.cosign/image"
	manifests := []ocispec.Descriptor{
		{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    fakeDigest("b"),
			Size:      100,
			Annotations: map[string]string{
				ocispec.AnnotationRefName: "example.com/repo:tagb",
				consts.KindAnnotationName: legacyKind,
			},
		},
	}
	buildMinimalOCILayout(t, dir, manifests)

	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}
	// Trigger LoadIndex via Walk.
	if err := o.Walk(func(_ string, _ ocispec.Descriptor) error { return nil }); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// Re-read index.json from disk and verify the annotation is unchanged.
	data, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index.json: %v", err)
	}
	for _, desc := range idx.Manifests {
		got := desc.Annotations[consts.KindAnnotationName]
		if got != legacyKind {
			t.Errorf("on-disk kind was mutated: got %q, want %q", got, legacyKind)
		}
	}
}

// --------------------------------------------------------------------------
// TestPush_NormalizesLegacyKindInStoredDescriptor
// --------------------------------------------------------------------------

// TestPush_NormalizesLegacyKindInStoredDescriptor verifies that after a Push()
// that matches the root digest, the descriptor stored in nameMap (and therefore
// returned by subsequent Walk() calls) carries the normalized dev.hauler/...
// kind annotation rather than the legacy value.
func TestPush_NormalizesLegacyKindInStoredDescriptor(t *testing.T) {
	dir := t.TempDir()
	buildMinimalOCILayout(t, dir, nil) // start with empty index

	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	// Build a minimal manifest blob so Push() can write it to disk.
	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    fakeDigest("config0"),
			Size:      2,
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestDigest := digest.FromBytes(manifestData)

	// Ensure the blobs directory exists so Push can write.
	blobsDir := filepath.Join(dir, ocispec.ImageBlobsDir, "sha256")
	if err := os.MkdirAll(blobsDir, 0755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}

	legacyKind := "dev.cosignproject.cosign/sigs"
	baseRef := "example.com/repo:tagsig"

	pusher, err := o.Pusher(context.Background(), baseRef+"@"+manifestDigest.String())
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestData)),
		Annotations: map[string]string{
			ocispec.AnnotationRefName: baseRef,
			consts.KindAnnotationName: legacyKind,
		},
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := w.Write(manifestData); err != nil {
		t.Fatalf("Write manifest: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	// Now Walk and verify the descriptor in nameMap has the normalized kind.
	// We need a fresh OCI instance so Walk calls LoadIndex (which reads SaveIndex output).
	o2, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI second: %v", err)
	}

	const legacyPrefix = "dev.cosignproject.cosign"
	const newPrefix = "dev.hauler"
	var found bool
	if err := o2.Walk(func(_ string, d ocispec.Descriptor) error {
		found = true
		kind := d.Annotations[consts.KindAnnotationName]
		if strings.HasPrefix(kind, legacyPrefix) {
			t.Errorf("Push stored descriptor with legacy kind %q; want normalized dev.hauler/... value", kind)
		}
		if !strings.HasPrefix(kind, newPrefix) {
			t.Errorf("Push stored descriptor with unexpected kind %q; want dev.hauler/... prefix", kind)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !found {
		t.Fatal("Walk returned no descriptors after Push")
	}

	// Also verify the caller's original descriptor map was NOT mutated.
	if desc.Annotations[consts.KindAnnotationName] != legacyKind {
		t.Errorf("Push mutated caller's descriptor annotations: got %q, want %q",
			desc.Annotations[consts.KindAnnotationName], legacyKind)
	}
}
