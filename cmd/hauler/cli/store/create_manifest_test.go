package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
)

// newCreateManifestOpts returns a CreateManifestOpts writing to a fresh file
// under t.TempDir().
func newCreateManifestOpts(t *testing.T, rso *flags.StoreRootOpts) *flags.CreateManifestOpts {
	t.Helper()
	return &flags.CreateManifestOpts{
		StoreRootOpts: rso,
		Output:        filepath.Join(t.TempDir(), "manifest.yaml"),
	}
}

// readManifest reads the manifest file at path, failing the test on error.
func readManifest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading manifest at %s: %v", path, err)
	}
	return string(data)
}

func TestCreateManifestCmd_EmptyStore(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	o := newCreateManifestOpts(t, defaultRootOpts(s.Root))

	err := CreateManifestCmd(ctx, o, s)
	if err == nil {
		t.Fatal("expected error for empty store, got nil")
	}
	if !strings.Contains(err.Error(), "no content to build a manifest from") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(o.Output); statErr == nil {
		t.Errorf("expected no manifest file to be written on error")
	}
}

func TestCreateManifestCmd_Image(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "test/repo", "v1", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/repo:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	if !strings.Contains(content, "kind: Images") {
		t.Errorf("expected an Images doc, got:\n%s", content)
	}
	if !strings.Contains(content, "name: "+host+"/test/repo:v1") {
		t.Errorf("expected image name %q in manifest, got:\n%s", host+"/test/repo:v1", content)
	}
	if strings.Contains(content, "rewrite:") {
		t.Errorf("did not expect a rewrite field for a never-rewritten image, got:\n%s", content)
	}
}

func TestCreateManifestCmd_ImageWithRewrite(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "src/repo", "v1", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	// storeImage with a rewrite target: the store ends up with the new ref as
	// the "current" annotations, but consts.OriginalRefAnnotation still holds
	// the original, pullable source ref captured at the initial add.
	if err := storeImage(ctx, s, v1.Image{Name: host + "/src/repo:v1"}, "", false, rso, ro, "newrepo/img:v2", "", false); err != nil {
		t.Fatalf("storeImage with rewrite: %v", err)
	}
	assertArtifactInStore(t, s, "newrepo/img:v2")

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	// The recovered name must be the original, pullable source ref...
	if !strings.Contains(content, "name: "+host+"/src/repo:v1") {
		t.Errorf("expected original ref %q recovered as name, got:\n%s", host+"/src/repo:v1", content)
	}
	// ...and rewrite must reproduce the store's actual current ref so a resync
	// recreates this exact layout.
	if !strings.Contains(content, "rewrite: "+host+"/newrepo/img:v2") {
		t.Errorf("expected rewrite %q in manifest, got:\n%s", host+"/newrepo/img:v2", content)
	}
}

func TestCreateManifestCmd_MultiPlatformIndexOmitsPlatform(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedIndex(t, host, "test/multiarch", "v1", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/multiarch:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage multi-arch index: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	if !strings.Contains(content, "name: "+host+"/test/multiarch:v1") {
		t.Errorf("expected multi-arch image name in manifest, got:\n%s", content)
	}
	// A stored index has no single unambiguous platform, so it must be left
	// unset rather than pinning one arbitrary arch.
	if strings.Contains(content, "platform:") {
		t.Errorf("did not expect a platform field for a multi-platform index, got:\n%s", content)
	}
}

func TestCreateManifestCmd_Chart(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	co := newAddChartOpts(chartTestdataDir, "")
	if err := AddChartCmd(ctx, co, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	if !strings.Contains(content, "kind: Charts") {
		t.Errorf("expected a Charts doc, got:\n%s", content)
	}
	if !strings.Contains(content, "name: rancher-cluster-templates") {
		t.Errorf("expected chart name in manifest, got:\n%s", content)
	}
	if !strings.Contains(content, "repoURL: "+chartTestdataDir) {
		t.Errorf("expected repoURL %q in manifest, got:\n%s", chartTestdataDir, content)
	}
	if strings.Contains(content, "NOTE: repoURL could not be recovered") {
		t.Errorf("did not expect the missing-repoURL note when repoURL is known, got:\n%s", content)
	}
}

func TestCreateManifestCmd_ChartMissingRepoURL(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	// A chart added from a bare local .tgz path with no RepoURL, mirroring how a
	// chart added without --repo has no recoverable source.
	chartDir := t.TempDir()
	tgzPath := seedChartWithImages(t, chartDir, nil)

	co := newAddChartOpts("", "")
	if err := AddChartCmd(ctx, co, s, tgzPath, rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	if !strings.Contains(content, "NOTE: repoURL could not be recovered from the store's metadata") {
		t.Errorf("expected missing-repoURL note, got:\n%s", content)
	}
	if !strings.Contains(content, "name: test-chart") {
		t.Errorf("expected chart name in manifest, got:\n%s", content)
	}
}

func TestCreateManifestCmd_ChartWithRewrite(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	co := newAddChartOpts(chartTestdataDir, "")
	co.Rewrite = "myorg/custom-chart"
	if err := AddChartCmd(ctx, co, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd with rewrite: %v", err)
	}
	assertArtifactInStore(t, s, "myorg/custom-chart")

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	// The recovered name/repoURL must be the original chart, not the rewritten one.
	if !strings.Contains(content, "name: rancher-cluster-templates") {
		t.Errorf("expected original chart name in manifest, got:\n%s", content)
	}
	if !strings.Contains(content, "repoURL: "+chartTestdataDir) {
		t.Errorf("expected original repoURL in manifest, got:\n%s", content)
	}
	if !strings.Contains(content, "rewrite: myorg/custom-chart") {
		t.Errorf("expected rewrite field in manifest, got:\n%s", content)
	}
}

func TestCreateManifestCmd_File(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	tmp, err := os.CreateTemp(t.TempDir(), "testfile-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("hello hauler") //nolint:errcheck
	tmp.Close()

	if err := storeFile(ctx, s, v1.File{Path: tmp.Name()}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	if !strings.Contains(content, "kind: Files") {
		t.Errorf("expected a Files doc, got:\n%s", content)
	}
	if !strings.Contains(content, "path: "+tmp.Name()) {
		t.Errorf("expected original local path %q recovered, got:\n%s", tmp.Name(), content)
	}
	if !strings.Contains(content, "name: "+filepath.Base(tmp.Name())) {
		t.Errorf("expected file name %q in manifest, got:\n%s", filepath.Base(tmp.Name()), content)
	}
}

func TestCreateManifestCmd_FileHTTP(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	url := seedFileInHTTPServer(t, "script.sh", "#!/bin/sh\necho ok")
	if err := storeFile(ctx, s, v1.File{Path: url}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	if !strings.Contains(content, "path: "+url) {
		t.Errorf("expected original URL %q recovered as path, got:\n%s", url, content)
	}
}

func TestCreateManifestCmd_MixedContent(t *testing.T) {
	ctx := newTestContext(t)
	host, rOpts := newLocalhostRegistry(t)
	seedImage(t, host, "test/repo", "v1", rOpts...)

	s := newTestStore(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := storeImage(ctx, s, v1.Image{Name: host + "/test/repo:v1"}, "", false, rso, ro, "", "", false); err != nil {
		t.Fatalf("storeImage: %v", err)
	}
	co := newAddChartOpts(chartTestdataDir, "")
	if err := AddChartCmd(ctx, co, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	tmp, err := os.CreateTemp(t.TempDir(), "testfile-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()
	if err := storeFile(ctx, s, v1.File{Path: tmp.Name()}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	o := newCreateManifestOpts(t, rso)
	if err := CreateManifestCmd(ctx, o, s); err != nil {
		t.Fatalf("CreateManifestCmd: %v", err)
	}

	content := readManifest(t, o.Output)
	for _, kind := range []string{"kind: Images", "kind: Charts", "kind: Files"} {
		if !strings.Contains(content, kind) {
			t.Errorf("expected %q doc in mixed-content manifest, got:\n%s", kind, content)
		}
	}
	if got := strings.Count(content, "---\n"); got != 3 {
		t.Errorf("expected 3 YAML documents, got %d in:\n%s", got, content)
	}
}

func TestStoreLacksProvenance(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "empty version", version: "", want: true},
		{name: "whitespace only", version: "   ", want: true},
		{name: "unparseable", version: "not-a-version", want: true},
		{name: "older patch", version: "v2.0.2", want: true},
		{name: "older minor", version: "v2.0.99", want: true},
		{name: "older major", version: "v1.9.9", want: true},
		{name: "pseudo-version before threshold", version: "v2.0.2-0.20260728211252-c6fbcc97b769+dirty", want: true},
		{name: "threshold exactly", version: "v2.1.0", want: false},
		{name: "threshold pre-release", version: "v2.1.0-rc1", want: false},
		{name: "newer patch", version: "v2.1.5", want: false},
		{name: "newer major", version: "v3.0.0", want: false},
		{name: "missing v prefix still parses", version: "2.0.2", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := storeLacksProvenance(tc.version); got != tc.want {
				t.Errorf("storeLacksProvenance(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}

func TestReadStoreHaulerVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	if err := os.WriteFile(path, []byte(`{"store-id":"abc","hauler-version":"v2.0.2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readStoreHaulerVersion(dir)
	if err != nil {
		t.Fatalf("readStoreHaulerVersion: %v", err)
	}
	if got != "v2.0.2" {
		t.Errorf("readStoreHaulerVersion = %q, want %q", got, "v2.0.2")
	}

	// A store.json with no hauler-version field yields an empty string (which the
	// caller treats as lacking provenance).
	if err := os.WriteFile(path, []byte(`{"store-id":"abc"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = readStoreHaulerVersion(dir)
	if err != nil {
		t.Fatalf("readStoreHaulerVersion (no version): %v", err)
	}
	if got != "" {
		t.Errorf("readStoreHaulerVersion (no version) = %q, want empty", got)
	}

	// A missing store.json is surfaced as an error.
	if _, err := readStoreHaulerVersion(t.TempDir()); err == nil {
		t.Error("expected error reading version from a directory with no store.json")
	}
}

func TestDecodeOriginalChartRef(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantRepoURL string
		wantTotal   string
	}{
		{
			name:        "repoURL and ref separated by pipe",
			in:          "https://charts.example.com|myrepo/mychart:1.0.0",
			wantRepoURL: "https://charts.example.com",
			wantTotal:   "myrepo/mychart:1.0.0",
		},
		{
			name:        "empty repoURL with leading pipe",
			in:          "|myrepo/mychart:1.0.0",
			wantRepoURL: "",
			wantTotal:   "myrepo/mychart:1.0.0",
		},
		{
			name:        "no pipe treated as bare ref with unknown repoURL",
			in:          "myrepo/mychart:1.0.0",
			wantRepoURL: "",
			wantTotal:   "myrepo/mychart:1.0.0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotRepoURL, gotTotal := decodeOriginalChartRef(tc.in)
			if gotRepoURL != tc.wantRepoURL || gotTotal != tc.wantTotal {
				t.Errorf("decodeOriginalChartRef(%q) = (%q, %q), want (%q, %q)",
					tc.in, gotRepoURL, gotTotal, tc.wantRepoURL, tc.wantTotal)
			}
		})
	}
}
