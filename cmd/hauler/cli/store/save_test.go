package store

// save_test.go covers writeExportsManifest and SaveCmd.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/archives"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/reference"
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

// newSaveOpts builds a SaveOpts pointing at storeDir with an absolute archive path.
func newSaveOpts(storeDir, archivePath string) *flags.SaveOpts {
	return &flags.SaveOpts{
		StoreRootOpts: defaultRootOpts(storeDir),
		FileName:      archivePath,
	}
}

// --------------------------------------------------------------------------
// writeExportsManifest unit tests
// --------------------------------------------------------------------------

func TestWriteExportsManifest(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("no platform filter includes all platforms", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)
		seedIndex(t, host, "test/multiarch", "v1", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/test/multiarch:v1", "", false, "", false, ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		if err := writeExportsManifest(ctx, s.Root, s.Root, ""); err != nil {
			t.Fatalf("writeExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) < 2 {
			t.Errorf("expected >=2 entries (all platforms), got %d", len(entries))
		}
	})

	t.Run("linux/amd64 filter yields single entry", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)
		seedIndex(t, host, "test/multiarch", "v2", rOpts...)

		s := newTestStore(t)
		if _, err := s.AddImage(ctx, host+"/test/multiarch:v2", "", false, "", false, ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		if err := writeExportsManifest(ctx, s.Root, s.Root, "linux/amd64"); err != nil {
			t.Fatalf("writeExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) != 1 {
			t.Errorf("expected 1 entry for linux/amd64, got %d", len(entries))
		}
	})

	t.Run("chart artifact excluded via config media type check", func(t *testing.T) {
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		o := newAddChartOpts(chartTestdataDir, "")
		if err := AddChartCmd(ctx, o, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
			t.Fatalf("AddChartCmd: %v", err)
		}

		if err := writeExportsManifest(ctx, s.Root, s.Root, ""); err != nil {
			t.Fatalf("writeExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) != 0 {
			t.Errorf("expected 0 entries (chart excluded from manifest.json), got %d", len(entries))
		}
	})
}

func TestWriteExportsManifest_DigestOnlyImageHasRepoTag(t *testing.T) {
	ctx := newTestContext(t)

	// Seed a tagged image so we can reference it by digest
	host, srcOpts := newLocalhostRegistry(t)
	img := seedImage(t, host, "test/digestonly", "v1", srcOpts...)
	hash, err := img.Digest()
	if err != nil {
		t.Fatalf("img.Digest: %v", err)
	}

	// Add the image BY DIGEST
	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/digestonly@"+hash.String(), "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage by digest: %v", err)
	}

	if err := writeExportsManifest(ctx, s.Root, s.Root, ""); err != nil {
		t.Fatalf("writeExportsManifest: %v", err)
	}

	entries := readManifestJSON(t, s.Root)
	if len(entries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(entries))
	}
	// Before fix 2, digest-only refs fall through the switch without setting RepoTags.
	if len(entries[0].RepoTags) == 0 {
		t.Errorf("expected at least one RepoTag for digest-only image, got none")
	}
}

func TestWriteExportsManifest_SkipsNonImages(t *testing.T) {
	ctx := newTestContext(t)

	url := seedFileInHTTPServer(t, "skip.sh", "#!/bin/sh\necho skip")
	s := newTestStore(t)
	if err := storeFile(ctx, s, v1.File{Path: url}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	if err := writeExportsManifest(ctx, s.Root, s.Root, ""); err != nil {
		t.Fatalf("writeExportsManifest: %v", err)
	}

	entries := readManifestJSON(t, s.Root)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for file-only store, got %d", len(entries))
	}
}

func TestWriteExportsManifest_DigestPinnedIndexUsesParentDigest(t *testing.T) {
	ctx := newTestContext(t)
	host, srcOpts := newLocalhostRegistry(t)
	idx := seedIndexWithUnknown(t, host, "test/digestindex", "v1", srcOpts...)
	parentHash, err := idx.Digest()
	if err != nil {
		t.Fatalf("idx.Digest: %v", err)
	}

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/digestindex@"+parentHash.String(), "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage by digest: %v", err)
	}

	outDir := t.TempDir()
	if err := writeExportsManifest(ctx, s.Root, outDir, ""); err != nil {
		t.Fatalf("writeExportsManifest: %v", err)
	}

	parentTag := strings.ReplaceAll(parentHash.String(), ":", "-")
	var tags []string
	for _, e := range readManifestJSON(t, outDir) {
		tags = append(tags, e.RepoTags...)
	}
	// No platform given: one child per known platform, each tagged from the PARENT
	// digest with an os-arch suffix. The child digests must appear nowhere.
	// referencev3.FamiliarName only strips the registry host for docker.io; this
	// test's host is a localhost test registry, so the expected tag keeps it.
	wantSuffixes := []string{"-linux-amd64", "-linux-arm64"}
	for _, sfx := range wantSuffixes {
		want := host + "/test/digestindex:" + parentTag + sfx
		if !slices.Contains(tags, want) {
			t.Errorf("missing tag %q in %v", want, tags)
		}
	}
	for _, tag := range tags {
		if !strings.Contains(tag, parentTag) {
			t.Errorf("tag %q not derived from parent digest %s", tag, parentHash.String())
		}
		if strings.Contains(tag, "-unknown-unknown") {
			t.Errorf("tag %q carries the unknown/unknown attestation child, which save.go's existing skip must exclude", tag)
		}
	}
}

func TestWriteExportsManifest_DigestPinnedIndexWithPlatform(t *testing.T) {
	ctx := newTestContext(t)
	host, srcOpts := newLocalhostRegistry(t)
	idx := seedIndex(t, host, "test/digestplat", "v1", srcOpts...)
	parentHash, err := idx.Digest()
	if err != nil {
		t.Fatalf("idx.Digest: %v", err)
	}

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/digestplat@"+parentHash.String(), "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage by digest: %v", err)
	}

	outDir := t.TempDir()
	if err := writeExportsManifest(ctx, s.Root, outDir, "linux/amd64"); err != nil {
		t.Fatalf("writeExportsManifest: %v", err)
	}

	entries := readManifestJSON(t, outDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with --platform, got %d", len(entries))
	}
	// See the FamiliarName note in TestWriteExportsManifest_DigestPinnedIndexUsesParentDigest.
	want := host + "/test/digestplat:" + strings.ReplaceAll(parentHash.String(), ":", "-")
	if !slices.Contains(entries[0].RepoTags, want) {
		t.Errorf("RepoTags = %v, want to contain %q", entries[0].RepoTags, want)
	}
}

// --------------------------------------------------------------------------
// writeContainerdIndexes unit tests
// --------------------------------------------------------------------------

func TestWriteContainerdIndexes(t *testing.T) {
	storeDir := t.TempDir()
	// Parent index entry with a legacy un-normalized name; a chart entry shaped
	// like what AddArtifact actually produces today (kind dev.hauler/image, no
	// io.containerd.image.name -- see writeContainerdIndexes's doc comment) that
	// must be filtered out on the missing-name half of the predicate; and a sigs
	// entry that must be filtered out on the kind half.
	storeIndex := `{
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.index.v1+json",
      "digest": "sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0",
      "size": 10200,
      "annotations": {
        "io.containerd.image.name": "index.docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0",
        "kind": "dev.hauler/imageIndex",
        "org.opencontainers.image.ref.name": "library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
      "size": 100,
      "annotations": {"kind": "dev.hauler/image", "org.opencontainers.image.ref.name": "charts/foo:1.0.0"}
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
      "size": 100,
      "annotations": {"kind": "dev.hauler/sigs", "io.containerd.image.name": "index.docker.io/library/busybox:sha256-498a.sig"}
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(storeDir, "index.json"), []byte(storeIndex), 0644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	dropped, err := writeContainerdIndexes(storeDir, outDir)
	if err != nil {
		t.Fatalf("writeContainerdIndexes: %v", err)
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}

	sidecar, err := os.ReadFile(filepath.Join(outDir, consts.HaulerIndexFile))
	if err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
	if string(sidecar) != storeIndex {
		t.Errorf("sidecar is not byte-identical to the store index")
	}

	var filtered ocispec.Index
	data, err := os.ReadFile(filepath.Join(outDir, "index.json"))
	if err != nil {
		t.Fatalf("filtered index missing: %v", err)
	}
	if err := json.Unmarshal(data, &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered.Manifests) != 1 {
		t.Fatalf("filtered manifests = %d, want 1", len(filtered.Manifests))
	}
	got := filtered.Manifests[0].Annotations[consts.ContainerdImageNameKey]
	want := "docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0"
	if got != want {
		t.Errorf("normalized name = %q, want %q", got, want)
	}
	// The store itself must be untouched.
	after, _ := os.ReadFile(filepath.Join(storeDir, "index.json"))
	if string(after) != storeIndex {
		t.Errorf("store index.json was modified")
	}
}

// TestWriteDefaultIndex covers the default-mode (non-`--containerd`) archive
// index rewrite added for #744 fix 1: every descriptor is kept (unlike
// writeContainerdIndexes' filtered rewrite), but io.containerd.image.name
// annotations still get normalized where present, so a default haul of a
// legacy store still carries CRI-resolvable names once oci-layout steers
// containerd onto the OCI import path. Reuses TestWriteContainerdIndexes'
// fixture shape.
func TestWriteDefaultIndex(t *testing.T) {
	storeDir := t.TempDir()
	storeIndex := `{
  "schemaVersion": 2,
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.index.v1+json",
      "digest": "sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0",
      "size": 10200,
      "annotations": {
        "io.containerd.image.name": "index.docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0",
        "kind": "dev.hauler/imageIndex",
        "org.opencontainers.image.ref.name": "library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
      "size": 100,
      "annotations": {"kind": "dev.hauler/image", "org.opencontainers.image.ref.name": "charts/foo:1.0.0"}
    },
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
      "size": 100,
      "annotations": {"kind": "dev.hauler/sigs", "io.containerd.image.name": "index.docker.io/library/busybox:sha256-498a.sig"}
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(storeDir, "index.json"), []byte(storeIndex), 0644); err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	if err := writeDefaultIndex(storeDir, outDir); err != nil {
		t.Fatalf("writeDefaultIndex: %v", err)
	}

	var got ocispec.Index
	data, err := os.ReadFile(filepath.Join(outDir, "index.json"))
	if err != nil {
		t.Fatalf("default index missing: %v", err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	var original ocispec.Index
	if err := json.Unmarshal([]byte(storeIndex), &original); err != nil {
		t.Fatal(err)
	}
	// Default mode is unfiltered: same descriptor count as the store's index,
	// unlike writeContainerdIndexes which drops the chart and sigs entries.
	if len(got.Manifests) != len(original.Manifests) {
		t.Fatalf("default index manifests = %d, want %d (unfiltered)", len(got.Manifests), len(original.Manifests))
	}

	wantParentName := "docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0"
	foundParent := false
	for _, d := range got.Manifests {
		if d.Digest.String() == "sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0" {
			foundParent = true
			if got := d.Annotations[consts.ContainerdImageNameKey]; got != wantParentName {
				t.Errorf("normalized parent name = %q, want %q", got, wantParentName)
			}
		}
	}
	if !foundParent {
		t.Fatal("parent index descriptor missing from default-mode index")
	}

	// The store itself must be untouched.
	after, _ := os.ReadFile(filepath.Join(storeDir, "index.json"))
	if string(after) != storeIndex {
		t.Errorf("store index.json was modified")
	}
}

// --------------------------------------------------------------------------
// SaveCmd integration tests
// --------------------------------------------------------------------------

func TestSaveCmd(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/save", "v1")

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/save:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	o := newSaveOpts(s.Root, archivePath)

	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	fi, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("archive is empty")
	}

	// Validate it is a well-formed zst archive by unarchiving it.
	destDir := t.TempDir()
	if err := archives.Unarchive(ctx, archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
}

func TestSaveCmd_ContainerdCompatibility(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/containerd-compat", "v1")

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/containerd-compat:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "haul-compat.tar.zst")
	o := newSaveOpts(s.Root, archivePath)
	o.ContainerdCompatibility = true

	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd ContainerdCompatibility: %v", err)
	}

	destDir := t.TempDir()
	if err := archives.Unarchive(ctx, archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	// oci-layout must be PRESENT so containerd takes its OCI import path (#744);
	// index.json is filtered; the full index rides as the sidecar.
	if _, err := os.Stat(filepath.Join(destDir, "oci-layout")); err != nil {
		t.Errorf("expected oci-layout present in containerd archive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, consts.HaulerIndexFile)); err != nil {
		t.Errorf("expected %s sidecar in containerd archive: %v", consts.HaulerIndexFile, err)
	}
	var filtered ocispec.Index
	data, err := os.ReadFile(filepath.Join(destDir, "index.json"))
	if err != nil {
		t.Fatalf("read archive index.json: %v", err)
	}
	if err := json.Unmarshal(data, &filtered); err != nil {
		t.Fatal(err)
	}
	for _, d := range filtered.Manifests {
		kind := d.Annotations[consts.KindAnnotationName]
		if kind != consts.KindAnnotationImage && kind != consts.KindAnnotationIndex {
			t.Errorf("non-image kind %q leaked into containerd index.json", kind)
		}
	}
}

// TestSaveCmd_ContainerdCompatibility_DigestPinnedMultiArch drives #744's own
// reproducer shape through the real pipeline in one pass: AddImage on a
// digest-pinned multi-arch index (with the unknown/unknown attestation child
// real registries attach) -> SaveCmd --containerd -> unarchive -> all three
// archive files. TestWriteContainerdIndexes and TestWriteExportsManifest_*
// exercise writeContainerdIndexes/writeExportsManifest directly against
// synthetic or single-purpose fixtures; nothing else routes a digest-pinned
// index through AddImage and SaveCmd together and inspects index.json,
// hauler-index.json, and manifest.json from the same archive. A non-image
// file artifact is included so the "zero non-image kinds" and dropped-count
// assertions are non-vacuous.
func TestSaveCmd_ContainerdCompatibility_DigestPinnedMultiArch(t *testing.T) {
	ctx := newTestContext(t)
	host, srcOpts := newLocalhostRegistry(t)
	idx := seedIndexWithUnknown(t, host, "test/save-multiarch-digest", "v1", srcOpts...)
	parentHash, err := idx.Digest()
	if err != nil {
		t.Fatalf("idx.Digest: %v", err)
	}

	s := newTestStore(t)
	imageRef := host + "/test/save-multiarch-digest@" + parentHash.String()
	if _, err := s.AddImage(ctx, imageRef, "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage by digest: %v", err)
	}
	fileURL := seedFileInHTTPServer(t, "companion.txt", "not an image")
	if err := storeFile(ctx, s, v1.File{Path: fileURL}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "haul-multiarch-digest.tar.zst")
	o := newSaveOpts(s.Root, archivePath)
	o.ContainerdCompatibility = true
	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd ContainerdCompatibility: %v", err)
	}

	destDir := t.TempDir()
	if err := archives.Unarchive(ctx, archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	// filtered index.json: exactly the parent index descriptor, normalized name, no
	// non-image kinds -- the attestation child and file artifact must both be gone.
	var filtered ocispec.Index
	data, err := os.ReadFile(filepath.Join(destDir, "index.json"))
	if err != nil {
		t.Fatalf("read archive index.json: %v", err)
	}
	if err := json.Unmarshal(data, &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered.Manifests) != 1 {
		t.Fatalf("filtered index.json manifests = %d, want 1 (parent index only)", len(filtered.Manifests))
	}
	parent := filtered.Manifests[0]
	if parent.Digest.String() != parentHash.String() {
		t.Errorf("filtered parent digest = %s, want %s", parent.Digest, parentHash)
	}
	if kind := parent.Annotations[consts.KindAnnotationName]; kind != consts.KindAnnotationIndex {
		t.Errorf("filtered parent kind = %q, want %q", kind, consts.KindAnnotationIndex)
	}
	wantContainerdName := reference.NormalizeContainerd(imageRef)
	if got := parent.Annotations[consts.ContainerdImageNameKey]; got != wantContainerdName {
		t.Errorf("filtered parent io.containerd.image.name = %q, want %q", got, wantContainerdName)
	}

	// The parent index blob itself must ride along in blobs/ -- spec §9.1's "parent
	// blob present" assertion, proven directly rather than only transitively via a
	// successful Unarchive.
	blobPath := filepath.Join(destDir, "blobs", parentHash.Algorithm, parentHash.Hex)
	if _, err := os.Stat(blobPath); err != nil {
		t.Errorf("parent index blob missing from archive at %s: %v", blobPath, err)
	}

	// hauler-index.json sidecar: parent index and the file artifact both present.
	sidecarData, err := os.ReadFile(filepath.Join(destDir, consts.HaulerIndexFile))
	if err != nil {
		t.Fatalf("read %s: %v", consts.HaulerIndexFile, err)
	}
	var sidecar ocispec.Index
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		t.Fatal(err)
	}
	if len(sidecar.Manifests) != 2 {
		t.Fatalf("%s manifests = %d, want 2 (parent index + file)", consts.HaulerIndexFile, len(sidecar.Manifests))
	}
	foundParent, foundFile := false, false
	for _, d := range sidecar.Manifests {
		if d.Digest.String() == parentHash.String() {
			foundParent = true
		}
		if strings.Contains(d.Annotations[ocispec.AnnotationRefName], "companion.txt") {
			foundFile = true
		}
	}
	if !foundParent {
		t.Errorf("%s missing parent index digest %s", consts.HaulerIndexFile, parentHash)
	}
	if !foundFile {
		t.Errorf("%s missing companion.txt file artifact", consts.HaulerIndexFile)
	}

	// manifest.json: every digest-pinned-index tag derives from the parent digest,
	// never a child's -- the #643/#744 regression this whole feature fixes.
	parentTag := strings.ReplaceAll(parentHash.String(), ":", "-")
	var tags []string
	for _, e := range readManifestJSON(t, destDir) {
		tags = append(tags, e.RepoTags...)
	}
	if len(tags) == 0 {
		t.Fatal("manifest.json has no RepoTags for the digest-pinned index")
	}
	for _, tag := range tags {
		if !strings.Contains(tag, parentTag) {
			t.Errorf("manifest.json tag %q not derived from parent digest %s", tag, parentHash)
		}
		if strings.Contains(tag, "-unknown-unknown") {
			t.Errorf("manifest.json tag %q carries the unknown/unknown attestation child", tag)
		}
	}
}

// snapshotDir maps every file under root (relative path) to its content hash.
func snapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		snap[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotDir: %v", err)
	}
	return snap
}

// TestSaveCmd_DoesNotMutateStore asserts save never mutates the live store,
// with one deliberate, documented exception: audit.log. At AuditLevel
// "standard" (the CLI default -- auditLevel in audit.go), Append writes a
// portable entry to <storeDir>/audit.log as designed (#727); that append is
// intentional and stays. The first two iterations run at AuditLevel "none"
// (defaultCliOpts) and must see zero delta, byte for byte. The third
// iteration runs at AuditLevel "standard" and must see exactly one delta:
// audit.log created or appended to -- nothing else in the store may move.
func TestSaveCmd_DoesNotMutateStore(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/no-mutate", "v1")

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/no-mutate:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	before := snapshotDir(t, s.Root)
	for _, containerd := range []bool{false, true} {
		o := newSaveOpts(s.Root, filepath.Join(t.TempDir(), "haul.tar.zst"))
		o.ContainerdCompatibility = containerd
		if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
			t.Fatalf("SaveCmd(containerd=%v): %v", containerd, err)
		}
		after := snapshotDir(t, s.Root)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("store mutated by save (containerd=%v):\nbefore: %v\nafter:  %v", containerd, before, after)
		}
	}

	t.Run("audit standard only touches audit.log", func(t *testing.T) {
		before := snapshotDir(t, s.Root)

		ro := defaultCliOpts()
		ro.AuditLevel = "standard"
		// Point the global audit write at a temp dir too, so this test never
		// touches the real $HOME/.hauler/audit.log -- see testhelpers_test.go's
		// note on defaultCliOpts for why AuditLevel defaults to "none" there.
		ro.HaulerDir = t.TempDir()

		o := newSaveOpts(s.Root, filepath.Join(t.TempDir(), "haul.tar.zst"))
		if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), ro); err != nil {
			t.Fatalf("SaveCmd(audit=standard): %v", err)
		}
		after := snapshotDir(t, s.Root)

		var changed []string
		for k, v := range after {
			if bv, ok := before[k]; !ok || bv != v {
				changed = append(changed, k)
			}
		}
		for k := range before {
			if _, ok := after[k]; !ok {
				t.Errorf("store file %q disappeared after save", k)
			}
		}
		if len(changed) != 1 || changed[0] != audit.LogFileName {
			t.Errorf("expected only %s to change, got: %v", audit.LogFileName, changed)
		}
	})
}

// TestSaveCmd_OutputInsideStoreDir proves the ReadDir skip on absOutputfile
// (#744 fold-in b): saving into a path inside the store dir a second time
// used to map the first save's own output file, which ArchiveFiles then
// deletes via RemoveAll before FilesFromDisk can read it -- erroring on a
// path that no longer exists.
func TestSaveCmd_OutputInsideStoreDir(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/output-in-store", "v1")

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/output-in-store:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	archivePath := filepath.Join(s.Root, "haul.tar.zst")
	o := newSaveOpts(s.Root, archivePath)

	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("first SaveCmd: %v", err)
	}
	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("second SaveCmd (output already present in store dir): %v", err)
	}
}

func TestSaveCmd_DefaultArchiveLayoutUnchanged(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/default-layout", "v1")

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/default-layout:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	if err := SaveCmd(ctx, newSaveOpts(s.Root, archivePath), s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	destDir := t.TempDir()
	if err := archives.Unarchive(ctx, archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	// oci-layout here is RESTORED, not preserved: v1 parity, but v2's store
	// never wrote one before this change (#744) -- see the scratch-marker
	// generation in SaveCmd. Don't assume a live store already has this file.
	for _, want := range []string{"index.json", "oci-layout", "manifest.json", "blobs"} {
		if _, err := os.Stat(filepath.Join(destDir, want)); err != nil {
			t.Errorf("default archive missing %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destDir, consts.HaulerIndexFile)); !os.IsNotExist(err) {
		t.Errorf("default archive must not contain the sidecar")
	}
}

func TestSaveCmd_EmptyStore(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// SaveCmd uses layout.FromPath which stats index.json — it must exist on
	// disk. A fresh store holds the index only in memory... SaveIndex flushes it.
	if err := s.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "haul-empty.tar.zst")
	o := newSaveOpts(s.Root, archivePath)

	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd empty store: %v", err)
	}

	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive not created for empty store: %v", err)
	}
}

// --------------------------------------------------------------------------
// parseChunkSize unit tests
// --------------------------------------------------------------------------

func TestParseChunkSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "kilobytes", input: "1K", want: 1 << 10},
		{name: "kilobytes long", input: "1KB", want: 1 << 10},
		{name: "megabytes", input: "500M", want: 500 << 20},
		{name: "megabytes long", input: "500MB", want: 500 << 20},
		{name: "gigabytes", input: "2G", want: 2 << 30},
		{name: "gigabytes long", input: "2GB", want: 2 << 30},
		{name: "terabytes", input: "1T", want: 1 << 40},
		{name: "terabytes long", input: "1TB", want: 1 << 40},
		{name: "plain bytes", input: "1024", want: 1024},
		{name: "lowercase", input: "1g", want: 1 << 30},
		{name: "whitespace trimmed", input: " 1G ", want: 1 << 30},
		{name: "zero is invalid", input: "0", wantErr: true},
		{name: "zero with suffix", input: "0M", wantErr: true},
		{name: "negative bytes", input: "-1", wantErr: true},
		{name: "negative with suffix", input: "-1G", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
		{name: "invalid suffix", input: "1X", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChunkSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseChunkSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseChunkSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SaveCmd chunk-size integration tests
// --------------------------------------------------------------------------

func TestSaveCmd_ChunkSize(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/chunksave", "v1")

	s := newTestStore(t)
	if _, err := s.AddImage(ctx, host+"/test/chunksave:v1", "", false, "", false, ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "haul-chunked.tar.zst")
	o := newSaveOpts(s.Root, archivePath)
	o.ChunkSize = "1K"

	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd with chunk-size: %v", err)
	}

	// original archive must be replaced by chunk files
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("original archive should be removed after chunking")
	}

	// at least one chunk must exist
	matches, err := filepath.Glob(filepath.Join(archiveDir, "haul-chunked.tar.zst.*"))
	if err != nil {
		t.Fatalf("glob chunks: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one chunk file, found none")
	}
}

func TestSaveCmd_ChunkSize_Invalid(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	if err := s.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	o := newSaveOpts(s.Root, filepath.Join(t.TempDir(), "haul.tar.zst"))
	o.ChunkSize = "0"

	if err := SaveCmd(ctx, o, s, defaultRootOpts(s.Root), defaultCliOpts()); err == nil {
		t.Fatal("SaveCmd: expected error for chunk-size=0, got nil")
	}
}
