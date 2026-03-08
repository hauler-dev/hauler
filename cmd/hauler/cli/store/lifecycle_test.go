package store

// lifecycle_test.go covers the end-to-end add->save->load->copy/extract lifecycle
// for file, image, and chart artifact types.
//
// Do NOT use t.Parallel() -- SaveCmd calls os.Chdir(storeDir).
// Always use absolute paths for StoreDir and FileName.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/store"
)

// TestLifecycle_FileArtifact_AddSaveLoadCopy exercises the full lifecycle for a
// file artifact: seed HTTP server -> storeFile -> SaveCmd -> LoadCmd -> CopyCmd dir://.
func TestLifecycle_FileArtifact_AddSaveLoadCopy(t *testing.T) {
	ctx := newTestContext(t)

	// Step 1: Seed an HTTP file server with known content.
	fileContent := "lifecycle file artifact content"
	url := seedFileInHTTPServer(t, "lifecycle.txt", fileContent)

	// Step 2: storeFile into store A.
	storeA := newTestStore(t)
	if err := storeFile(ctx, storeA, v1.File{Path: url}); err != nil {
		t.Fatalf("storeFile: %v", err)
	}
	assertArtifactInStore(t, storeA, "lifecycle.txt")

	// Flush index.json so SaveCmd can read it from disk.
	if err := storeA.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	// Step 3: SaveCmd -> archive (absolute paths required).
	archivePath := filepath.Join(t.TempDir(), "lifecycle-file.tar.zst")
	saveOpts := newSaveOpts(storeA.Root, archivePath)
	if err := SaveCmd(ctx, saveOpts, defaultRootOpts(storeA.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	fi, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("archive is empty")
	}

	// Step 4: LoadCmd -> store B.
	storeBDir := t.TempDir()
	loadOpts := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(storeBDir),
		FileName:      []string{archivePath},
	}
	if err := LoadCmd(ctx, loadOpts, defaultRootOpts(storeBDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	storeB, err := store.NewLayout(storeBDir)
	if err != nil {
		t.Fatalf("store.NewLayout(storeB): %v", err)
	}
	assertArtifactInStore(t, storeB, "lifecycle.txt")

	// Step 5: CopyCmd dir:// -> extract file to destDir.
	extractDir := t.TempDir()
	copyOpts := &flags.CopyOpts{StoreRootOpts: defaultRootOpts(storeB.Root)}
	if err := CopyCmd(ctx, copyOpts, storeB, "dir://"+extractDir, defaultCliOpts()); err != nil {
		t.Fatalf("CopyCmd dir: %v", err)
	}

	// Step 6: Assert file content matches original.
	outPath := filepath.Join(extractDir, "lifecycle.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected extracted file at %s: %v", outPath, err)
	}
	if string(data) != fileContent {
		t.Errorf("file content mismatch: got %q, want %q", string(data), fileContent)
	}
}

// TestLifecycle_Image_AddSaveLoadCopyRegistry exercises the full lifecycle for
// a container image: seed registry 1 -> storeImage -> SaveCmd -> LoadCmd ->
// CopyCmd registry:// -> verify in registry 2.
func TestLifecycle_Image_AddSaveLoadCopyRegistry(t *testing.T) {
	ctx := newTestContext(t)

	// Step 1: Seed image into in-memory registry 1.
	srcHost, srcOpts := newLocalhostRegistry(t)
	srcImg := seedImage(t, srcHost, "lifecycle/app", "v1", srcOpts...)

	srcDigest, err := srcImg.Digest()
	if err != nil {
		t.Fatalf("srcImg.Digest: %v", err)
	}

	// Step 2: storeImage into store A.
	storeA := newTestStore(t)
	rso := defaultRootOpts(storeA.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, storeA, v1.Image{Name: srcHost + "/lifecycle/app:v1"}, "", rso, ro, ""); err != nil {
		t.Fatalf("storeImage: %v", err)
	}
	assertArtifactInStore(t, storeA, "lifecycle/app:v1")

	// Flush index.json for SaveCmd.
	if err := storeA.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	// Step 3: SaveCmd -> archive.
	archivePath := filepath.Join(t.TempDir(), "lifecycle-image.tar.zst")
	saveOpts := newSaveOpts(storeA.Root, archivePath)
	if err := SaveCmd(ctx, saveOpts, defaultRootOpts(storeA.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	// Step 4: LoadCmd -> store B.
	storeBDir := t.TempDir()
	loadOpts := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(storeBDir),
		FileName:      []string{archivePath},
	}
	if err := LoadCmd(ctx, loadOpts, defaultRootOpts(storeBDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	storeB, err := store.NewLayout(storeBDir)
	if err != nil {
		t.Fatalf("store.NewLayout(storeB): %v", err)
	}
	assertArtifactInStore(t, storeB, "lifecycle/app:v1")

	// Step 5: CopyCmd registry:// -> in-memory registry 2.
	dstHost, dstOpts := newTestRegistry(t)
	copyOpts := &flags.CopyOpts{
		StoreRootOpts: defaultRootOpts(storeB.Root),
		PlainHTTP:     true,
	}
	if err := CopyCmd(ctx, copyOpts, storeB, "registry://"+dstHost, defaultCliOpts()); err != nil {
		t.Fatalf("CopyCmd registry: %v", err)
	}

	// Step 6: Pull from registry 2 and compare digest to original.
	dstRef, err := name.NewTag(dstHost+"/lifecycle/app:v1", name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	desc, err := remote.Get(dstRef, dstOpts...)
	if err != nil {
		t.Fatalf("image not found in target registry: %v", err)
	}
	if desc.Digest != srcDigest {
		t.Errorf("digest mismatch: got %s, want %s", desc.Digest, srcDigest)
	}
}

// TestLifecycle_Chart_AddSaveLoadExtract exercises the full lifecycle for a
// Helm chart: AddChartCmd -> SaveCmd -> LoadCmd -> ExtractCmd -> .tgz in destDir.
func TestLifecycle_Chart_AddSaveLoadExtract(t *testing.T) {
	ctx := newTestContext(t)

	// Step 1: AddChartCmd with local testdata chart into store A.
	storeA := newTestStore(t)
	rso := defaultRootOpts(storeA.Root)
	ro := defaultCliOpts()

	chartOpts := newAddChartOpts(chartTestdataDir, "")
	if err := AddChartCmd(ctx, chartOpts, storeA, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	assertArtifactInStore(t, storeA, "rancher-cluster-templates")

	// Flush index.json for SaveCmd.
	if err := storeA.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	// Step 2: SaveCmd -> archive.
	archivePath := filepath.Join(t.TempDir(), "lifecycle-chart.tar.zst")
	saveOpts := newSaveOpts(storeA.Root, archivePath)
	if err := SaveCmd(ctx, saveOpts, defaultRootOpts(storeA.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	// Step 3: LoadCmd -> new store.
	storeBDir := t.TempDir()
	loadOpts := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(storeBDir),
		FileName:      []string{archivePath},
	}
	if err := LoadCmd(ctx, loadOpts, defaultRootOpts(storeBDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	storeB, err := store.NewLayout(storeBDir)
	if err != nil {
		t.Fatalf("store.NewLayout(storeB): %v", err)
	}
	assertArtifactInStore(t, storeB, "rancher-cluster-templates")

	// Step 4: ExtractCmd -> .tgz in destDir.
	destDir := t.TempDir()
	extractOpts := &flags.ExtractOpts{
		StoreRootOpts:  defaultRootOpts(storeB.Root),
		DestinationDir: destDir,
	}
	if err := ExtractCmd(ctx, extractOpts, storeB, "hauler/rancher-cluster-templates:0.5.2"); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tgz") || strings.HasSuffix(e.Name(), ".tar.gz") {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected a .tgz or .tar.gz in destDir, got: %v", names)
	}
}

// TestLifecycle_Remove_ThenSave verifies that removing one artifact from a store
// with two file artifacts, then saving/loading, results in only the retained
// artifact being present.
func TestLifecycle_Remove_ThenSave(t *testing.T) {
	ctx := newTestContext(t)

	// Step 1: Add two file artifacts.
	url1 := seedFileInHTTPServer(t, "keep-me.txt", "content to keep")
	url2 := seedFileInHTTPServer(t, "remove-me.txt", "content to remove")

	storeA := newTestStore(t)
	if err := storeFile(ctx, storeA, v1.File{Path: url1}); err != nil {
		t.Fatalf("storeFile keep-me: %v", err)
	}
	if err := storeFile(ctx, storeA, v1.File{Path: url2}); err != nil {
		t.Fatalf("storeFile remove-me: %v", err)
	}

	if n := countArtifactsInStore(t, storeA); n != 2 {
		t.Fatalf("expected 2 artifacts after adding both files, got %d", n)
	}

	// Step 2: RemoveCmd(Force:true) on the "remove-me" artifact.
	if err := RemoveCmd(ctx, &flags.RemoveOpts{Force: true}, storeA, "remove-me"); err != nil {
		t.Fatalf("RemoveCmd: %v", err)
	}

	if n := countArtifactsInStore(t, storeA); n != 1 {
		t.Fatalf("expected 1 artifact after removal, got %d", n)
	}
	assertArtifactInStore(t, storeA, "keep-me.txt")

	// Flush index.json for SaveCmd. RemoveCmd calls OCI.SaveIndex() internally
	// (via Layout.Remove), but call it again for safety.
	if err := storeA.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	// Step 3: SaveCmd -> archive.
	archivePath := filepath.Join(t.TempDir(), "lifecycle-remove.tar.zst")
	saveOpts := newSaveOpts(storeA.Root, archivePath)
	if err := SaveCmd(ctx, saveOpts, defaultRootOpts(storeA.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	// Step 4: LoadCmd -> new store.
	storeBDir := t.TempDir()
	loadOpts := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(storeBDir),
		FileName:      []string{archivePath},
	}
	if err := LoadCmd(ctx, loadOpts, defaultRootOpts(storeBDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	storeB, err := store.NewLayout(storeBDir)
	if err != nil {
		t.Fatalf("store.NewLayout(storeB): %v", err)
	}

	// Step 5: Assert only the retained artifact is present.
	if n := countArtifactsInStore(t, storeB); n != 1 {
		t.Errorf("expected 1 artifact in loaded store, got %d", n)
	}
	assertArtifactInStore(t, storeB, "keep-me.txt")
}
