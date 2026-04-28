package store

// copy_test.go covers CopyCmd for both registry:// and dir:// targets.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
)

// --------------------------------------------------------------------------
// Error / guard tests
// --------------------------------------------------------------------------

// TestCopyCmd_EmptyStoreFails verifies that CopyCmd returns an error when the
// store has no index.json on disk (i.e. nothing has been added yet).
func TestCopyCmd_EmptyStoreFails(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t) // freshly created — index.json not yet on disk

	o := &flags.CopyOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	err := CopyCmd(ctx, o, s, "registry://127.0.0.1:5000", defaultCliOpts())
	if err == nil {
		t.Fatal("expected error for empty store, got nil")
	}
	if !strings.Contains(err.Error(), "store index not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCopyCmd_DeprecatedCredentials verifies that passing Username returns the
// deprecation error before any other check.
func TestCopyCmd_DeprecatedCredentials(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	o := &flags.CopyOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		Username:      "user",
		Password:      "pass",
	}
	err := CopyCmd(ctx, o, s, "registry://127.0.0.1:5000", defaultCliOpts())
	if err == nil {
		t.Fatal("expected deprecation error, got nil")
	}
	if !strings.Contains(err.Error(), "deprecated") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCopyCmd_UnknownProtocol verifies that an unrecognized scheme returns an
// error containing "detecting protocol".
func TestCopyCmd_UnknownProtocol(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	// Write index.json so IndexExists() passes.
	if err := s.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	o := &flags.CopyOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	err := CopyCmd(ctx, o, s, "ftp://somehost/path", defaultCliOpts())
	if err == nil {
		t.Fatal("expected error for unknown protocol, got nil")
	}
	if !strings.Contains(err.Error(), "detecting protocol") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --------------------------------------------------------------------------
// Registry copy tests
// --------------------------------------------------------------------------

// TestCopyCmd_Registry seeds a store with a single image, copies it to an
// in-memory target registry, and verifies the image is reachable there.
func TestCopyCmd_Registry(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	seedImage(t, srcHost, "test/copy", "v1")

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/test/copy:v1"}, "", false, rso, ro, ""); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	dstHost, dstOpts := newTestRegistry(t)
	o := &flags.CopyOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		PlainHTTP:     true,
	}
	if err := CopyCmd(ctx, o, s, "registry://"+dstHost, ro); err != nil {
		t.Fatalf("CopyCmd registry: %v", err)
	}

	// Verify the image is reachable in the target registry.
	dstRef, err := name.NewTag(dstHost+"/test/copy:v1", name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag: %v", err)
	}
	if _, err := remote.Get(dstRef, dstOpts...); err != nil {
		t.Errorf("image not found in target registry after copy: %v", err)
	}
}

// TestCopyCmd_Registry_OnlyFilter seeds two images in distinct repos, copies
// with --only=repo1, and asserts only repo1 reaches the target.
func TestCopyCmd_Registry_OnlyFilter(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	seedImage(t, srcHost, "myorg/repo1", "v1")
	seedImage(t, srcHost, "myorg/repo2", "v1")

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	for _, repo := range []string{"myorg/repo1:v1", "myorg/repo2:v1"} {
		if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/" + repo}, "", false, rso, ro, ""); err != nil {
			t.Fatalf("storeImage %s: %v", repo, err)
		}
	}

	dstHost, dstOpts := newTestRegistry(t)
	o := &flags.CopyOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		PlainHTTP:     true,
		Only:          "repo1",
	}
	if err := CopyCmd(ctx, o, s, "registry://"+dstHost, ro); err != nil {
		t.Fatalf("CopyCmd with --only: %v", err)
	}

	// repo1 must be in target.
	ref1, err := name.NewTag(dstHost+"/myorg/repo1:v1", name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag repo1: %v", err)
	}
	if _, err := remote.Get(ref1, dstOpts...); err != nil {
		t.Errorf("repo1 should be in target registry but was not found: %v", err)
	}

	// repo2 must NOT be in target.
	ref2, err := name.NewTag(dstHost+"/myorg/repo2:v1", name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag repo2: %v", err)
	}
	if _, err := remote.Get(ref2, dstOpts...); err == nil {
		t.Error("repo2 should NOT be in target registry after --only=repo1, but was found")
	}
}

// TestCopyCmd_Registry_SigTagDerivation seeds a base image along with cosign
// v2 signature artifacts, adds everything to the store via AddImage (which
// auto-discovers the .sig/.att/.sbom tags), then copies to a target registry
// and verifies the sig arrives at the expected sha256-<hex>.sig tag.
func TestCopyCmd_Registry_SigTagDerivation(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	srcImg := seedImage(t, srcHost, "test/signed", "v1")
	seedCosignV2Artifacts(t, srcHost, "test/signed", srcImg)

	// AddImage discovers and stores the .sig/.att/.sbom tags automatically.
	s := newTestStore(t)
	if err := s.AddImage(ctx, srcHost+"/test/signed:v1", "", false); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	dstHost, dstOpts := newTestRegistry(t)
	o := &flags.CopyOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		PlainHTTP:     true,
	}
	if err := CopyCmd(ctx, o, s, "registry://"+dstHost, defaultCliOpts()); err != nil {
		t.Fatalf("CopyCmd: %v", err)
	}

	// Compute the expected cosign sig tag from the image's manifest digest.
	hash, err := srcImg.Digest()
	if err != nil {
		t.Fatalf("srcImg.Digest: %v", err)
	}
	sigTag := strings.ReplaceAll(hash.String(), ":", "-") + ".sig"

	sigRef, err := name.NewTag(dstHost+"/test/signed:"+sigTag, name.Insecure)
	if err != nil {
		t.Fatalf("name.NewTag sigRef: %v", err)
	}
	if _, err := remote.Get(sigRef, dstOpts...); err != nil {
		t.Errorf("sig not found at expected tag %s in target registry: %v", sigTag, err)
	}
}

// TestCopyCmd_Registry_IgnoreErrors verifies that a push failure to a
// non-listening address is swallowed when IgnoreErrors is set.
func TestCopyCmd_Registry_IgnoreErrors(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	seedImage(t, srcHost, "test/ignore", "v1")

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/test/ignore:v1"}, "", false, rso, ro, ""); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	// localhost:1 is a port that is never listening.
	o := &flags.CopyOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		PlainHTTP:     true,
	}
	roIgnore := defaultCliOpts()
	roIgnore.IgnoreErrors = true
	if err := CopyCmd(ctx, o, s, "registry://localhost:1", roIgnore); err != nil {
		t.Errorf("expected no error with IgnoreErrors=true, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// Directory copy tests
// --------------------------------------------------------------------------

// TestCopyCmd_Dir_Files copies a file artifact to a directory target and
// verifies the file appears under its original basename.
func TestCopyCmd_Dir_Files(t *testing.T) {
	ctx := newTestContext(t)

	content := "hello from hauler file"
	url := seedFileInHTTPServer(t, "data.txt", content)

	s := newTestStore(t)
	if err := storeFile(ctx, s, v1.File{Path: url}, true); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	destDir := t.TempDir()
	o := &flags.CopyOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := CopyCmd(ctx, o, s, "dir://"+destDir, defaultCliOpts()); err != nil {
		t.Fatalf("CopyCmd dir: %v", err)
	}

	outPath := filepath.Join(destDir, "data.txt")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("file not found in destDir after dir copy: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: got %q, want %q", string(data), content)
	}
}

// TestCopyCmd_Dir_SkipsImages verifies that container images are not extracted
// when copying to a directory target.
func TestCopyCmd_Dir_SkipsImages(t *testing.T) {
	ctx := newTestContext(t)

	srcHost, _ := newLocalhostRegistry(t)
	seedImage(t, srcHost, "test/imgskip", "v1")

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, s, v1.Image{Name: srcHost + "/test/imgskip:v1"}, "", false, rso, ro, ""); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	destDir := t.TempDir()
	o := &flags.CopyOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := CopyCmd(ctx, o, s, "dir://"+destDir, ro); err != nil {
		t.Fatalf("CopyCmd dir: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected empty destDir for image-only store, found: %s", strings.Join(names, ", "))
	}
}

// TestCopyCmd_Dir_Charts copies a local Helm chart artifact to a directory
// target and verifies a .tgz file is present.
func TestCopyCmd_Dir_Charts(t *testing.T) {
	ctx := newTestContext(t)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	o := newAddChartOpts(chartTestdataDir, "")
	if err := AddChartCmd(ctx, o, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}

	destDir := t.TempDir()
	copyOpts := &flags.CopyOpts{StoreRootOpts: defaultRootOpts(s.Root)}
	if err := CopyCmd(ctx, copyOpts, s, "dir://"+destDir, ro); err != nil {
		t.Fatalf("CopyCmd dir charts: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var found bool
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
		t.Errorf("no .tgz found in destDir after chart copy; found: %v", names)
	}
}
