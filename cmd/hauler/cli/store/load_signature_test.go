package store

// load_signature_test.go pins the consumer-facing contract of LoadCmd: the
// 4-arg form (no pre-built *store.Layout), the store construction happening
// inside LoadCmd, the audit entry it appends, and the fact that the loader
// does not write an oci-layout marker.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/flags"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// TestLoadCmd_ConsumerSignature pins the 4-arg consumer signature at compile
// time. A future drift (e.g. re-adding the *store.Layout parameter) fails this
// assignment and therefore the build.
func TestLoadCmd_ConsumerSignature(t *testing.T) {
	var _ func(context.Context, *flags.LoadOpts, *flags.StoreRootOpts, *flags.CliRootOpts) error = LoadCmd
}

// TestLoadCmd_NoPreBuiltLayoutResolvesAndLoads verifies that a call with no
// pre-built *store.Layout resolves the store dir from the root opts, loads the
// named haul into it, and returns nil.
func TestLoadCmd_NoPreBuiltLayoutResolvesAndLoads(t *testing.T) {
	ctx := newTestContext(t)
	destDir := t.TempDir()

	o := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(destDir),
		FileName:      []string{testHaulArchive},
	}
	if err := LoadCmd(ctx, o, defaultRootOpts(destDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	// the store dir must exist and carry the loaded artifacts
	loaded, err := store.NewLayout(destDir)
	if err != nil {
		t.Fatalf("re-open store: %v", err)
	}
	if countArtifactsInStore(t, loaded) == 0 {
		t.Error("expected artifacts in store after LoadCmd with no pre-built layout")
	}
}

// TestLoadCmd_DoesNotWriteOCILayout verifies the loader does not write the
// oci-layout marker; the consumer backfills it after LoadCmd.
func TestLoadCmd_DoesNotWriteOCILayout(t *testing.T) {
	ctx := newTestContext(t)
	destDir := t.TempDir()

	o := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(destDir),
		FileName:      []string{testHaulArchive},
	}
	if err := LoadCmd(ctx, o, defaultRootOpts(destDir), defaultCliOpts()); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "oci-layout")); !os.IsNotExist(err) {
		t.Errorf("oci-layout should be absent after LoadCmd, stat err = %v", err)
	}
}

// TestLoadCmd_AuditEntryCarriesStoreIDAndRoot verifies that when auditing is
// enabled a loaded haul appends an entry carrying the store's StoreID and Root.
func TestLoadCmd_AuditEntryCarriesStoreIDAndRoot(t *testing.T) {
	ctx := newTestContext(t)
	destDir := t.TempDir()

	ro := defaultCliOpts()
	ro.AuditLevel = "standard"
	ro.HaulerDir = t.TempDir()

	o := &flags.LoadOpts{
		StoreRootOpts: defaultRootOpts(destDir),
		FileName:      []string{testHaulArchive},
	}
	if err := LoadCmd(ctx, o, defaultRootOpts(destDir), ro); err != nil {
		t.Fatalf("LoadCmd: %v", err)
	}

	entry := lastAuditEntry(t, ro.HaulerDir)
	if entry.StoreID == "" {
		t.Error("audit entry StoreID is empty, want the store's StoreID")
	}
	if entry.Store == "" {
		t.Error("audit entry Store is empty, want the store's Root")
	}
	if entry.Command != "store load" {
		t.Errorf("audit entry Command = %q, want %q", entry.Command, "store load")
	}
}

// lastAuditEntry reads <haulerDir>/audit.log and returns its last JSON line.
func lastAuditEntry(t *testing.T, haulerDir string) struct {
	StoreID   string `json:"store-id"`
	Store     string `json:"store"`
	Command   string `json:"command"`
	Reference string `json:"reference"`
} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(haulerDir, "audit.log"))
	if err != nil {
		t.Fatalf("reading audit.log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatalf("audit.log has no entries")
	}
	var entry struct {
		StoreID   string `json:"store-id"`
		Store     string `json:"store"`
		Command   string `json:"command"`
		Reference string `json:"reference"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("unmarshaling last audit.log line: %v\nline: %s", err, lines[len(lines)-1])
	}
	return entry
}
