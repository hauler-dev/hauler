package store

// load_test.go covers unarchiveLayoutTo, LoadCmd, and clearDir.
//
// Do NOT call t.Parallel() on tests that invoke createRootLevelArchive —
// that helper uses the mholt/archives library directly to avoid os.Chdir,
// so it is safe for concurrent use, but the tests themselves exercise
// unarchiveLayoutTo which is already sequential.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mholtarchives "github.com/mholt/archives"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/store"
)

// testHaulArchive is the relative path from cmd/hauler/cli/store/ to the
// bundled test haul archive produced by the project's own CI/test setup.
const testHaulArchive = "../../../../testdata/haul.tar.zst"

// createRootLevelArchive creates a tar.zst archive from dir with files placed
// at the archive root (no directory prefix). This matches the layout produced
// by SaveCmd, which uses os.Chdir + Archive(".", ...) to achieve the same
// effect. Using mholt/archives directly avoids the os.Chdir side-effect.
func createRootLevelArchive(dir, outfile string) error {
	// A trailing path separator tells mholt/archives to enumerate the
	// directory's *contents* only — files land at archive root with no prefix.
	// Without the trailing slash, an empty value uses filepath.Base(dir) as
	// the archive subdirectory name instead of placing files at root.
	files, err := mholtarchives.FilesFromDisk(context.Background(), nil, map[string]string{
		dir + string(filepath.Separator): "",
	})
	if err != nil {
		return err
	}

	f, err := os.Create(outfile)
	if err != nil {
		return err
	}
	defer f.Close()

	format := mholtarchives.CompressedArchive{
		Compression: mholtarchives.Zstd{},
		Archival:    mholtarchives.Tar{},
	}
	return format.Archive(context.Background(), f, files)
}

// --------------------------------------------------------------------------
// TestUnarchiveLayoutTo
// --------------------------------------------------------------------------

// TestUnarchiveLayoutTo verifies that unarchiveLayoutTo correctly extracts a
// haul archive into a destination OCI layout, backfills missing annotations,
// and propagates the ContainerdImageNameKey → ImageRefKey mapping.
func TestUnarchiveLayoutTo(t *testing.T) {
	ctx := newTestContext(t)
	destDir := t.TempDir()
	tempDir := t.TempDir()

	if err := unarchiveLayoutTo(ctx, testHaulArchive, destDir, tempDir); err != nil {
		t.Fatalf("unarchiveLayoutTo: %v", err)
	}

	s, err := store.NewLayout(destDir)
	if err != nil {
		t.Fatalf("store.NewLayout(destDir): %v", err)
	}

	if count := countArtifactsInStore(t, s); count == 0 {
		t.Fatal("expected at least one descriptor in dest store after unarchiveLayoutTo")
	}

	// Every top-level descriptor must carry KindAnnotationName.
	// Descriptors that were loaded with ContainerdImageNameKey must also have
	// ImageRefKey set (the backfill logic in unarchiveLayoutTo ensures this).
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if desc.Annotations[consts.KindAnnotationName] == "" {
			t.Errorf("descriptor %s missing KindAnnotationName", desc.Digest)
		}
		if _, hasContainerd := desc.Annotations[consts.ContainerdImageNameKey]; hasContainerd {
			if desc.Annotations[consts.ImageRefKey] == "" {
				t.Errorf("descriptor %s has %s but missing %s",
					desc.Digest, consts.ContainerdImageNameKey, consts.ImageRefKey)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
}

// --------------------------------------------------------------------------
// TestLoadCmd_LocalFile
// --------------------------------------------------------------------------

// TestLoadCmd_LocalFile verifies that LoadCmd loads one or more local haul
// archives into the destination store.
func TestLoadCmd_LocalFile(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("single archive", func(t *testing.T) {
		destDir := t.TempDir()
		o := &flags.LoadOpts{
			StoreRootOpts: defaultRootOpts(destDir),
			FileName:      []string{testHaulArchive},
		}
		if err := LoadCmd(ctx, o, defaultRootOpts(destDir), defaultCliOpts()); err != nil {
			t.Fatalf("LoadCmd: %v", err)
		}
		s, err := store.NewLayout(destDir)
		if err != nil {
			t.Fatalf("store.NewLayout: %v", err)
		}
		if countArtifactsInStore(t, s) == 0 {
			t.Error("expected artifacts in store after LoadCmd")
		}
	})

	t.Run("two archives", func(t *testing.T) {
		// Loading the same archive twice must be idempotent: duplicate blobs are
		// silently discarded by the OCI pusher. The descriptor count after two
		// loads must equal the count after a single load.
		singleDir := t.TempDir()
		singleOpts := &flags.LoadOpts{
			StoreRootOpts: defaultRootOpts(singleDir),
			FileName:      []string{testHaulArchive},
		}
		if err := LoadCmd(ctx, singleOpts, defaultRootOpts(singleDir), defaultCliOpts()); err != nil {
			t.Fatalf("LoadCmd single: %v", err)
		}
		singleStore, err := store.NewLayout(singleDir)
		if err != nil {
			t.Fatalf("store.NewLayout single: %v", err)
		}
		singleCount := countArtifactsInStore(t, singleStore)

		doubleDir := t.TempDir()
		doubleOpts := &flags.LoadOpts{
			StoreRootOpts: defaultRootOpts(doubleDir),
			FileName:      []string{testHaulArchive, testHaulArchive},
		}
		if err := LoadCmd(ctx, doubleOpts, defaultRootOpts(doubleDir), defaultCliOpts()); err != nil {
			t.Fatalf("LoadCmd double: %v", err)
		}
		doubleStore, err := store.NewLayout(doubleDir)
		if err != nil {
			t.Fatalf("store.NewLayout double: %v", err)
		}
		doubleCount := countArtifactsInStore(t, doubleStore)

		if doubleCount != singleCount {
			t.Errorf("loading the same archive twice: got %d descriptors, want %d (same as single load)",
				doubleCount, singleCount)
		}
	})
}

// --------------------------------------------------------------------------
// TestLoadCmd_RemoteArchive
// --------------------------------------------------------------------------

// TestLoadCmd_RemoteArchive verifies that LoadCmd can fetch and load a haul
// archive served over HTTP.
func TestLoadCmd_RemoteArchive(t *testing.T) {
	ctx := newTestContext(t)

	archiveData, err := os.ReadFile(testHaulArchive)
	if err != nil {
		t.Fatalf("read test archive: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(archiveData) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	destDir := t.TempDir()
	remoteURL := srv.URL + "/haul.tar.zst"

	o := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(destDir),
		FileName:      []string{remoteURL},
	}

	if err := LoadCmd(ctx, o, defaultRootOpts(destDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd remote: %v", err)
	}

	s, err := store.NewLayout(destDir)
	if err != nil {
		t.Fatalf("store.NewLayout: %v", err)
	}
	if countArtifactsInStore(t, s) == 0 {
		t.Error("expected artifacts in store after remote LoadCmd")
	}
}

// --------------------------------------------------------------------------
// TestUnarchiveLayoutTo_AnnotationBackfill
// --------------------------------------------------------------------------

// TestUnarchiveLayoutTo_AnnotationBackfill crafts a haul archive whose
// index.json entries are missing KindAnnotationName, then verifies that
// unarchiveLayoutTo backfills every entry with KindAnnotationImage.
func TestUnarchiveLayoutTo_AnnotationBackfill(t *testing.T) {
	ctx := newTestContext(t)

	// Step 1: Extract the real test archive to obtain a valid OCI layout on disk.
	extractDir := t.TempDir()
	if err := archives.Unarchive(ctx, testHaulArchive, extractDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	// Step 2: Read index.json and strip KindAnnotationName from every descriptor.
	indexPath := filepath.Join(extractDir, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}

	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index.json: %v", err)
	}
	if len(idx.Manifests) == 0 {
		t.Skip("testdata/haul.tar.zst has no top-level manifests — cannot test backfill")
	}
	for i := range idx.Manifests {
		delete(idx.Manifests[i].Annotations, consts.KindAnnotationName)
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal stripped index.json: %v", err)
	}
	if err := os.WriteFile(indexPath, out, 0644); err != nil {
		t.Fatalf("write stripped index.json: %v", err)
	}

	// Step 3: Re-archive with files at the archive root (no subdir prefix) so
	// the layout matches what unarchiveLayoutTo expects after extraction.
	strippedArchive := filepath.Join(t.TempDir(), "stripped.tar.zst")
	if err := createRootLevelArchive(extractDir, strippedArchive); err != nil {
		t.Fatalf("createRootLevelArchive: %v", err)
	}

	// Step 4: Load the stripped archive.
	destDir := t.TempDir()
	tempDir := t.TempDir()
	if err := unarchiveLayoutTo(ctx, strippedArchive, destDir, tempDir); err != nil {
		t.Fatalf("unarchiveLayoutTo stripped: %v", err)
	}

	// Step 5: Every descriptor in the dest store must now have
	// KindAnnotationName set to KindAnnotationImage (the backfill default).
	s, err := store.NewLayout(destDir)
	if err != nil {
		t.Fatalf("store.NewLayout(destDir): %v", err)
	}

	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		kind := desc.Annotations[consts.KindAnnotationName]
		if kind == "" {
			t.Errorf("descriptor %s missing KindAnnotationName after backfill", desc.Digest)
		} else if kind != consts.KindAnnotationImage {
			t.Errorf("descriptor %s: expected backfilled kind=%q, got %q",
				desc.Digest, consts.KindAnnotationImage, kind)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
}

// --------------------------------------------------------------------------
// TestUnarchiveLayoutTo_LegacyKindMigration
// --------------------------------------------------------------------------

// TestUnarchiveLayoutTo_LegacyKindMigration crafts a haul archive whose
// index.json contains old dev.cosignproject.cosign kind values, then verifies
// that unarchiveLayoutTo translates them to dev.hauler equivalents.
func TestUnarchiveLayoutTo_LegacyKindMigration(t *testing.T) {
	ctx := newTestContext(t)

	// Step 1: Extract the real test archive to obtain a valid OCI layout on disk.
	extractDir := t.TempDir()
	if err := archives.Unarchive(ctx, testHaulArchive, extractDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	// Step 2: Read index.json and inject old dev.cosignproject.cosign kind values.
	indexPath := filepath.Join(extractDir, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}

	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index.json: %v", err)
	}
	if len(idx.Manifests) == 0 {
		t.Skip("testdata/haul.tar.zst has no top-level manifests — cannot test legacy kind migration")
	}

	// Replace all kind annotations with old-prefix equivalents so we can verify
	// that unarchiveLayoutTo normalizes them to the new dev.hauler prefix.
	const legacyPrefix = "dev.cosignproject.cosign"
	const newPrefix = "dev.hauler"
	for i := range idx.Manifests {
		if idx.Manifests[i].Annotations == nil {
			idx.Manifests[i].Annotations = make(map[string]string)
		}
		kind := idx.Manifests[i].Annotations[consts.KindAnnotationName]
		if kind == "" {
			kind = consts.KindAnnotationImage
		}
		// Rewrite dev.hauler/* → dev.cosignproject.cosign/* to simulate legacy archive.
		if strings.HasPrefix(kind, newPrefix) {
			kind = legacyPrefix + kind[len(newPrefix):]
		}
		idx.Manifests[i].Annotations[consts.KindAnnotationName] = kind
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy index.json: %v", err)
	}
	if err := os.WriteFile(indexPath, out, 0644); err != nil {
		t.Fatalf("write legacy index.json: %v", err)
	}

	// Step 3: Re-archive with files at the archive root (no subdir prefix).
	legacyArchive := filepath.Join(t.TempDir(), "legacy.tar.zst")
	if err := createRootLevelArchive(extractDir, legacyArchive); err != nil {
		t.Fatalf("createRootLevelArchive: %v", err)
	}

	// Step 4: Load the legacy archive.
	destDir := t.TempDir()
	tempDir := t.TempDir()
	if err := unarchiveLayoutTo(ctx, legacyArchive, destDir, tempDir); err != nil {
		t.Fatalf("unarchiveLayoutTo legacy: %v", err)
	}

	// Step 5: Every descriptor in the dest store must now have a dev.hauler kind.
	s, err := store.NewLayout(destDir)
	if err != nil {
		t.Fatalf("store.NewLayout(destDir): %v", err)
	}

	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		kind := desc.Annotations[consts.KindAnnotationName]
		if strings.HasPrefix(kind, legacyPrefix) {
			t.Errorf("descriptor %s still has legacy kind %q; expected dev.hauler prefix",
				desc.Digest, kind)
		}
		if !strings.HasPrefix(kind, newPrefix) {
			t.Errorf("descriptor %s has unexpected kind %q; expected dev.hauler prefix",
				desc.Digest, kind)
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
}

// --------------------------------------------------------------------------
// TestClearDir
// --------------------------------------------------------------------------

// TestClearDir verifies that clearDir removes all entries from a directory
// without removing the directory itself.
func TestClearDir(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	if err := clearDir(dir); err != nil {
		t.Fatalf("clearDir: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir after clearDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("clearDir: expected empty dir, found: %s", strings.Join(names, ", "))
	}
}

// --------------------------------------------------------------------------
// TestSynthesizeContainerdImageName
// --------------------------------------------------------------------------

// TestSynthesizeContainerdImageName tests the annotation synthesis logic
// in unarchiveLayoutTo when io.containerd.image.name is absent.
func TestSynthesizeContainerdImageName(t *testing.T) {
	tests := []struct {
		name               string
		inputAnnotations   map[string]string
		wantContainerdName string
		wantImageRef       string
	}{
		{
			name:               "entry with io.containerd.image.name present — pass-through unchanged",
			inputAnnotations:   map[string]string{consts.ContainerdImageNameKey: "library/nginx:latest"},
			wantContainerdName: "library/nginx:latest",
			wantImageRef:       "nginx:latest", // Strips at first /
		},
		{
			name:               "entry with org.opencontainers.image.ref.name only — synthesize from it",
			inputAnnotations:   map[string]string{"org.opencontainers.image.ref.name": "localhost:5000/library/nginx:latest"},
			wantContainerdName: "library/nginx:latest", // Strips registry prefix
			wantImageRef:       "library/nginx:latest", // Set from synthesized ContainerdImageNameKey
		},
		{
			name:               "entry with io.containerd.image.ref.name only — copy verbatim",
			inputAnnotations:   map[string]string{"io.containerd.image.ref.name": "myregistry.io/some/image:v1"},
			wantContainerdName: "myregistry.io/some/image:v1", // Verbatim, no stripping
			wantImageRef:       "",                            // Not set (io.containerd.image.ref.name is verbatim, no ImageRefKey)
		},
		{
			name:               "entry with neither annotation — leave io.containerd.image.name absent",
			inputAnnotations:   map[string]string{},
			wantContainerdName: "",
			wantImageRef:       "",
		},
		{
			name:               "entry with empty org.opencontainers.image.ref.name — not a valid fallback",
			inputAnnotations:   map[string]string{"org.opencontainers.image.ref.name": ""},
			wantContainerdName: "",
			wantImageRef:       "",
		},
		{
			name:               "entry with both fallback annotations — primary wins",
			inputAnnotations:   map[string]string{"org.opencontainers.image.ref.name": "registry.com/image:v1", "io.containerd.image.ref.name": "other.io/image:v2"},
			wantContainerdName: "image:v1", // Strips registry prefix
			wantImageRef:       "image:v1", // Set from synthesized ContainerdImageNameKey
		},
		{
			name:               "entry with org.opencontainers.image.ref.name = single-slash path — minimal strip",
			inputAnnotations:   map[string]string{"org.opencontainers.image.ref.name": "library/nginx:latest"},
			wantContainerdName: "nginx:latest", // Strips at first /
			wantImageRef:       "nginx:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal index.json with the test annotations
			idx := ocispec.Index{
				Manifests: []ocispec.Descriptor{
					{
						MediaType:   ocispec.MediaTypeImageManifest,
						Annotations: tt.inputAnnotations,
					},
				},
			}

			// Apply the normalization logic (same as in unarchiveLayoutTo)
			for i := range idx.Manifests {
				if idx.Manifests[i].Annotations == nil {
					idx.Manifests[i].Annotations = make(map[string]string)
				}
				if _, exists := idx.Manifests[i].Annotations[consts.KindAnnotationName]; !exists {
					idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.KindAnnotationImage
				}
				kind := idx.Manifests[i].Annotations[consts.KindAnnotationName]
				idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.NormalizeLegacyKind(kind)
				if ref, ok := idx.Manifests[i].Annotations[consts.ContainerdImageNameKey]; ok {
					if slash := strings.Index(ref, "/"); slash != -1 {
						ref = ref[slash+1:]
					}
					if idx.Manifests[i].Annotations[consts.ImageRefKey] != ref {
						idx.Manifests[i].Annotations[consts.ImageRefKey] = ref
					}
				} else {
					// Synthesize io.containerd.image.name from fallback annotation keys.
					var synthesizedRef string
					if refName, ok := idx.Manifests[i].Annotations["org.opencontainers.image.ref.name"]; ok && refName != "" {
						// Strip registry prefix like existing code does for ImageRefKey
						if slash := strings.Index(refName, "/"); slash != -1 {
							refName = refName[slash+1:]
						}
						synthesizedRef = refName
					} else if refName, ok := idx.Manifests[i].Annotations["io.containerd.image.ref.name"]; ok && refName != "" {
						synthesizedRef = refName
					}
					if synthesizedRef != "" {
						idx.Manifests[i].Annotations[consts.ContainerdImageNameKey] = synthesizedRef
						// For org.opencontainers.image.ref.name fallback, also set ImageRefKey
						// to match the existing behavior when ContainerdImageNameKey is present
						if refName, ok := idx.Manifests[i].Annotations["org.opencontainers.image.ref.name"]; ok && refName != "" {
							idx.Manifests[i].Annotations[consts.ImageRefKey] = synthesizedRef
						}
					}
				}
			}

			// Verify results
			got := idx.Manifests[0].Annotations
			if gotContainerdName := got[consts.ContainerdImageNameKey]; gotContainerdName != tt.wantContainerdName {
				t.Errorf("ContainerdImageNameKey = %q, want %q", gotContainerdName, tt.wantContainerdName)
			}
			if gotImageRef := got[consts.ImageRefKey]; gotImageRef != tt.wantImageRef {
				t.Errorf("ImageRefKey = %q, want %q", gotImageRef, tt.wantImageRef)
			}
		})
	}
}

// TestSynthesizeContainerdImageName_VerbatimFallback tests the secondary fallback
// using io.containerd.image.ref.name (no registry prefix stripping).
func TestSynthesizeContainerdImageName_VerbatimFallback(t *testing.T) {
	idx := ocispec.Index{
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Annotations: map[string]string{
					"io.containerd.image.ref.name": "myregistry.io/some/image:v1",
				},
			},
		},
	}

	for i := range idx.Manifests {
		if idx.Manifests[i].Annotations == nil {
			idx.Manifests[i].Annotations = make(map[string]string)
		}
		if _, exists := idx.Manifests[i].Annotations[consts.KindAnnotationName]; !exists {
			idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.KindAnnotationImage
		}
		kind := idx.Manifests[i].Annotations[consts.KindAnnotationName]
		idx.Manifests[i].Annotations[consts.KindAnnotationName] = consts.NormalizeLegacyKind(kind)
		if ref, ok := idx.Manifests[i].Annotations[consts.ContainerdImageNameKey]; ok {
			if slash := strings.Index(ref, "/"); slash != -1 {
				ref = ref[slash+1:]
			}
			if idx.Manifests[i].Annotations[consts.ImageRefKey] != ref {
				idx.Manifests[i].Annotations[consts.ImageRefKey] = ref
			}
		} else {
			if refName, ok := idx.Manifests[i].Annotations["org.opencontainers.image.ref.name"]; ok && refName != "" {
				if slash := strings.Index(refName, "/"); slash != -1 {
					refName = refName[slash+1:]
				}
				idx.Manifests[i].Annotations[consts.ContainerdImageNameKey] = refName
			} else if refName, ok := idx.Manifests[i].Annotations["io.containerd.image.ref.name"]; ok && refName != "" {
				idx.Manifests[i].Annotations[consts.ContainerdImageNameKey] = refName
			}
		}
	}

	got := idx.Manifests[0].Annotations
	if gotContainerdName := got[consts.ContainerdImageNameKey]; gotContainerdName != "myregistry.io/some/image:v1" {
		t.Errorf("ContainerdImageNameKey = %q, want %q", gotContainerdName, "myregistry.io/some/image:v1")
	}
	// ImageRefKey should NOT be set when using io.containerd.image.ref.name as fallback
	if gotImageRef := got[consts.ImageRefKey]; gotImageRef != "" {
		t.Errorf("ImageRefKey = %q, want empty (io.containerd.image.ref.name is verbatim)", gotImageRef)
	}
}
