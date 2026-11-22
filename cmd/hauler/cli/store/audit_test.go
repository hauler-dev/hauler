package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/archives"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// auditLine is the subset of an audit log line the tests below check
type auditLine struct {
	AuditID   string   `json:"audit-id"`
	Timestamp string   `json:"timestamp"`
	Command   string   `json:"command"`
	Type      string   `json:"type"`
	Reference string   `json:"reference"`
	Digest    string   `json:"digest"`
	Artifacts []string `json:"artifacts"`
	HaulID    string   `json:"haul-id"`
}

// readAuditLines returns every line of dir's audit.log, raw and decoded
func readAuditLines(t *testing.T, dir string) ([]string, []auditLine) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, audit.LogFileName))
	if err != nil {
		t.Fatalf("reading audit.log in [%s]: %v", dir, err)
	}
	raw := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	lines := make([]auditLine, len(raw))
	for i, r := range raw {
		if err := json.Unmarshal([]byte(r), &lines[i]); err != nil {
			t.Fatalf("decoding audit line %q: %v", r, err)
		}
	}
	return raw, lines
}

// commandLines returns the decoded lines whose command matches
func commandLines(lines []auditLine, command string) []auditLine {
	var out []auditLine
	for _, l := range lines {
		if l.Command == command {
			out = append(out, l)
		}
	}
	return out
}

// auditCliOpts returns CliRootOpts at audit level standard with the global log in a temp dir
func auditCliOpts(t *testing.T) *flags.CliRootOpts {
	t.Helper()
	ro := defaultCliOpts()
	ro.AuditLevel = "standard"
	ro.HaulerDir = t.TempDir()
	return ro
}

// newChartStore returns a store holding the testdata chart and the chart's digest
func newChartStore(t *testing.T) (*store.Layout, string) {
	t.Helper()
	ctx := newTestContext(t)
	s := newTestStore(t)
	if err := AddChartCmd(ctx, newAddChartOpts(chartTestdataDir, ""), s, "rancher-cluster-templates-0.5.2.tgz", defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	d := storedDigest(t, s, "rancher-cluster-templates")
	if d == "" {
		t.Fatalf("chart not found in store")
	}
	return s, d
}

func TestSaveCmd_AuditEntryShipsInHaul(t *testing.T) {
	ctx := newTestContext(t)
	s, chartDigest := newChartStore(t)
	ro := auditCliOpts(t)

	haul := filepath.Join(t.TempDir(), "haul.tar.zst")
	if err := SaveCmd(ctx, newSaveOpts(s.Root, haul), s, defaultRootOpts(s.Root), ro); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	unpacked := t.TempDir()
	if err := archives.Unarchive(ctx, haul, unpacked); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	haulRaw, haulLines := readAuditLines(t, unpacked)
	saved := haulLines[len(haulLines)-1]
	if saved.Command != "store save" {
		t.Fatalf("last haul audit line is %q, want the save entry", saved.Command)
	}
	if !slices.Equal(saved.Artifacts, []string{chartDigest}) {
		t.Errorf("save artifacts = %v, want [%s]", saved.Artifacts, chartDigest)
	}
	if saved.Reference != "haul.tar.zst" {
		t.Errorf("portable save reference = %q, want the haul's base name", saved.Reference)
	}
	if got := audit.HaulID(unpacked); got != saved.AuditID {
		t.Errorf("HaulID = %q, want the save's audit-id %q", got, saved.AuditID)
	}

	// the store's own log gets the same line once the save succeeds
	storeRaw, _ := readAuditLines(t, s.Root)
	if storeRaw[len(storeRaw)-1] != haulRaw[len(haulRaw)-1] {
		t.Errorf("store and haul save lines differ:\nstore: %s\nhaul:  %s", storeRaw[len(storeRaw)-1], haulRaw[len(haulRaw)-1])
	}
}

func TestSaveCmd_AuditNoneShipsStoreLogUnchanged(t *testing.T) {
	ctx := newTestContext(t)
	s, _ := newChartStore(t)
	existing := `{"audit-id":"earlier","command":"store add chart"}` + "\n"
	if err := os.WriteFile(filepath.Join(s.Root, audit.LogFileName), []byte(existing), 0o644); err != nil {
		t.Fatalf("seed store audit.log: %v", err)
	}

	haul := filepath.Join(t.TempDir(), "haul.tar.zst")
	if err := SaveCmd(ctx, newSaveOpts(s.Root, haul), s, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}
	unpacked := t.TempDir()
	if err := archives.Unarchive(ctx, haul, unpacked); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(unpacked, audit.LogFileName))
	if err != nil {
		t.Fatalf("haul has no audit.log: %v", err)
	}
	if string(data) != existing {
		t.Errorf("haul audit.log = %q, want the store's log unchanged %q", data, existing)
	}
}

// newAuditedTestHaul builds a haul holding one image whose audit.log is built from the image's digest
func newAuditedTestHaul(t *testing.T, log func(digest string) string) string {
	t.Helper()
	dir := t.TempDir()
	p, err := layout.Write(dir, empty.Index)
	if err != nil {
		t.Fatalf("layout.Write: %v", err)
	}
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	if err := p.AppendImage(img, layout.WithAnnotations(map[string]string{consts.ContainerdImageNameKey: "index.docker.io/library/audit:latest"})); err != nil {
		t.Fatalf("AppendImage: %v", err)
	}
	d, err := img.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, audit.LogFileName), []byte(log(d.String())), 0o644); err != nil {
		t.Fatalf("write haul audit.log: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	if err := createRootLevelArchive(dir, archivePath); err != nil {
		t.Fatalf("createRootLevelArchive: %v", err)
	}
	return archivePath
}

func TestLoadCmd_AuditRecordsHaulID(t *testing.T) {
	saveLine := func(id string, artifacts string) func(string) string {
		return func(digest string) string {
			return fmt.Sprintf(`{"audit-id":"%s","command":"store save","reference":"haul.tar.zst","artifacts":[%s]}`+"\n", id, strings.ReplaceAll(artifacts, "DIGEST", digest))
		}
	}
	cases := map[string]struct {
		haul       func(t *testing.T) string
		wantHaulID string
	}{
		"haul with a staged save entry": {
			haul: func(t *testing.T) string {
				return newAuditedTestHaul(t, saveLine("from-the-connected-side", `"DIGEST"`))
			},
			wantHaulID: "from-the-connected-side",
		},
		"haul ending with an earlier save's entry": {
			haul:       func(t *testing.T) string { return newAuditedTestHaul(t, saveLine("earlier-save", `"sha256:0000"`)) },
			wantHaulID: "",
		},
		"haul from an older hauler": {
			haul: func(t *testing.T) string {
				return newAuditedTestHaul(t, func(string) string {
					return `{"audit-id":"old","command":"store save","reference":"haul.tar.zst"}` + "\n"
				})
			},
			wantHaulID: "",
		},
		"haul without an audit log": {
			haul:       newTestHaul,
			wantHaulID: "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := newTestContext(t)
			s := newTestStore(t)
			ro := auditCliOpts(t)
			o := &flags.LoadOpts{StoreRootOpts: defaultRootOpts(s.Root), FileName: []string{tc.haul(t)}}
			if err := LoadCmd(ctx, o, s, defaultRootOpts(s.Root), ro); err != nil {
				t.Fatalf("LoadCmd: %v", err)
			}
			_, lines := readAuditLines(t, s.Root)
			loads := commandLines(lines, "store load")
			if len(loads) != 1 {
				t.Fatalf("got %d store load lines, want 1", len(loads))
			}
			if loads[0].HaulID != tc.wantHaulID {
				t.Errorf("load haul-id = %q, want %q", loads[0].HaulID, tc.wantHaulID)
			}
		})
	}
}

func TestSyncCmd_AuditManifestEntry(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	ro := auditCliOpts(t)

	manifest := filepath.Join(t.TempDir(), "charts.yaml")
	content := fmt.Sprintf("apiVersion: content.hauler.cattle.io/v1\nkind: Charts\nmetadata:\n  name: test-charts\nspec:\n  charts:\n    - name: rancher-cluster-templates-0.5.2.tgz\n      repoURL: %s\n", chartTestdataDir)
	if err := os.WriteFile(manifest, []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	o := newSyncOpts(s.Root)
	o.FileName = []string{manifest}
	if err := SyncCmd(ctx, o, s, defaultRootOpts(s.Root), ro); err != nil {
		t.Fatalf("SyncCmd: %v", err)
	}

	_, lines := readAuditLines(t, s.Root)
	syncs := commandLines(lines, "store sync")
	if len(syncs) != 1 {
		t.Fatalf("got %d store sync lines, want 1: %+v", len(syncs), lines)
	}
	if syncs[0].Type != "manifest" || syncs[0].Reference != "charts.yaml" || syncs[0].Digest != fileDigest(manifest) {
		t.Errorf("sync line = %+v, want type manifest, reference charts.yaml, and the manifest's digest", syncs[0])
	}
	// the synced chart keeps its own add line, tied to the sync by audit-id
	adds := commandLines(lines, "store add chart")
	if len(adds) != 1 || adds[0].AuditID != syncs[0].AuditID {
		t.Errorf("want one store add chart line sharing the sync's audit-id, got %+v", adds)
	}
}

func TestCopyCmd_AuditDirectoryEntry(t *testing.T) {
	ctx := newTestContext(t)
	s, chartDigest := newChartStore(t)
	ro := auditCliOpts(t)

	dest := filepath.Join(t.TempDir(), "extracted")
	if err := CopyCmd(ctx, &flags.CopyOpts{StoreRootOpts: defaultRootOpts(s.Root)}, s, "dir://"+dest, ro); err != nil {
		t.Fatalf("CopyCmd: %v", err)
	}

	_, lines := readAuditLines(t, s.Root)
	copies := commandLines(lines, "store copy")
	if len(copies) != 1 {
		t.Fatalf("got %d store copy lines, want 1", len(copies))
	}
	if copies[0].Reference != "dir://extracted" {
		t.Errorf("portable copy reference = %q, want dir://extracted", copies[0].Reference)
	}
	if !slices.Equal(copies[0].Artifacts, []string{chartDigest}) {
		t.Errorf("copy artifacts = %v, want [%s]", copies[0].Artifacts, chartDigest)
	}

	// the global log keeps the full local path
	_, global := readAuditLines(t, ro.HaulerDir)
	if g := commandLines(global, "store copy"); len(g) != 1 || g[0].Reference != "dir://"+dest {
		t.Errorf("global copy lines = %+v, want one with reference dir://%s", g, dest)
	}
}

func TestServe_InternalCopyNotAudited(t *testing.T) {
	ctx := newTestContext(t)
	s, _ := newChartStore(t)
	ro := auditCliOpts(t)

	if _, err := extractGitRepos(ctx, s, t.TempDir(), ro); err != nil {
		t.Fatalf("extractGitRepos: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, audit.LogFileName)); !os.IsNotExist(err) {
		_, lines := readAuditLines(t, s.Root)
		t.Errorf("serve's internal copy wrote audit lines: %+v", lines)
	}
}

func TestExtractCmd_AuditEntry(t *testing.T) {
	ctx := newTestContext(t)
	s, chartDigest := newChartStore(t)
	ro := auditCliOpts(t)

	eo := &flags.ExtractOpts{StoreRootOpts: defaultRootOpts(s.Root), DestinationDir: t.TempDir()}
	if err := ExtractCmd(ctx, eo, s, "hauler/rancher-cluster-templates:0.5.2", ro); err != nil {
		t.Fatalf("ExtractCmd: %v", err)
	}

	_, lines := readAuditLines(t, s.Root)
	extracts := commandLines(lines, "store extract")
	if len(extracts) != 1 {
		t.Fatalf("got %d store extract lines, want 1", len(extracts))
	}
	if extracts[0].Reference != "hauler/rancher-cluster-templates:0.5.2" || !slices.Equal(extracts[0].Artifacts, []string{chartDigest}) {
		t.Errorf("extract line = %+v, want the chart reference and [%s]", extracts[0], chartDigest)
	}
}
