package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// TestStoreID_PersistsAcrossNewLayoutCalls verifies the store-id survives repeated NewLayout calls
func TestStoreID_PersistsAcrossNewLayoutCalls(t *testing.T) {
	root := t.TempDir()

	s1, err := store.NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	if s1.StoreID == "" {
		t.Fatal("expected a non-empty StoreID on first creation")
	}

	metaPath := filepath.Join(root, consts.DefaultStoreMetadataName)
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("expected %s to be written: %v", metaPath, err)
	}

	s2, err := store.NewLayout(root)
	if err != nil {
		t.Fatalf("second NewLayout: %v", err)
	}
	if s2.StoreID != s1.StoreID {
		t.Errorf("StoreID changed across NewLayout calls: first=%s second=%s", s1.StoreID, s2.StoreID)
	}
}

// TestStoreID_MalformedMetadataRegenerates verifies a corrupt store.json gets a fresh id
func TestStoreID_MalformedMetadataRegenerates(t *testing.T) {
	root := t.TempDir()
	metaPath := filepath.Join(root, consts.DefaultStoreMetadataName)
	if err := os.WriteFile(metaPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := store.NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	if s.StoreID == "" {
		t.Fatal("expected a freshly generated StoreID when metadata is malformed")
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m struct {
		StoreID string `json:"store-id"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("regenerated metadata is not valid JSON: %v", err)
	}
	if m.StoreID != s.StoreID {
		t.Errorf("persisted store-id %q does not match Layout.StoreID %q", m.StoreID, s.StoreID)
	}
}

// TestStoreID_MissingFieldRegenerates verifies a store.json missing store-id gets a fresh id
func TestStoreID_MissingFieldRegenerates(t *testing.T) {
	root := t.TempDir()
	metaPath := filepath.Join(root, consts.DefaultStoreMetadataName)
	if err := os.WriteFile(metaPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := store.NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	if s.StoreID == "" {
		t.Fatal("expected a freshly generated StoreID when store-id field is missing")
	}
}

// TestResolveStoreID covers exact-id, prefix, ambiguous-prefix, and no-match resolution
func TestResolveStoreID(t *testing.T) {
	haulerDir := t.TempDir()

	type entry struct {
		Path    string `json:"path"`
		Updated string `json:"updated"`
	}
	inv := map[string]entry{
		"ec520cf6-e01b-4d6f-93ea-6588de0d5159": {Path: "/store/a", Updated: "2026-01-01T00:00:00Z"},
		"ec520000-0000-0000-0000-000000000000": {Path: "/store/b", Updated: "2026-01-01T00:00:00Z"},
		"deadbeef-dead-beef-dead-beefdeadbeef": {Path: "/store/c", Updated: "2026-01-01T00:00:00Z"},
	}
	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(haulerDir, consts.DefaultStoreInventoryName), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("exact id match", func(t *testing.T) {
		id, path, err := store.ResolveStoreID(haulerDir, "deadbeef-dead-beef-dead-beefdeadbeef")
		if err != nil {
			t.Fatalf("ResolveStoreID: %v", err)
		}
		if id != "deadbeef-dead-beef-dead-beefdeadbeef" || path != "/store/c" {
			t.Errorf("got id=%s path=%s, want id=deadbeef-dead-beef-dead-beefdeadbeef path=/store/c", id, path)
		}
	})

	t.Run("unambiguous prefix match", func(t *testing.T) {
		id, path, err := store.ResolveStoreID(haulerDir, "deadbeef")
		if err != nil {
			t.Fatalf("ResolveStoreID: %v", err)
		}
		if id != "deadbeef-dead-beef-dead-beefdeadbeef" || path != "/store/c" {
			t.Errorf("got id=%s path=%s, want id=deadbeef-dead-beef-dead-beefdeadbeef path=/store/c", id, path)
		}
	})

	t.Run("ambiguous prefix returns error", func(t *testing.T) {
		_, _, err := store.ResolveStoreID(haulerDir, "ec52")
		if err == nil {
			t.Fatal("expected error for ambiguous prefix, got nil")
		}
	})

	t.Run("no match returns error", func(t *testing.T) {
		_, _, err := store.ResolveStoreID(haulerDir, "abc12345")
		if err == nil {
			t.Fatal("expected error when no store matches, got nil")
		}
	})
}

// TestNewLayout_UpdatesInventory verifies WithHaulerDir registers the store in stores.json
func TestNewLayout_UpdatesInventory(t *testing.T) {
	haulerDir := t.TempDir()
	storeDir := t.TempDir()

	s, err := store.NewLayout(storeDir, store.WithHaulerDir(haulerDir))
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	id, path, err := store.ResolveStoreID(haulerDir, s.StoreID)
	if err != nil {
		t.Fatalf("ResolveStoreID: %v", err)
	}
	if id != s.StoreID {
		t.Errorf("resolved id = %s, want %s", id, s.StoreID)
	}
	if path != storeDir {
		t.Errorf("resolved path = %s, want %s", path, storeDir)
	}
	if !store.MatchesStoreID(path, id) {
		t.Error("MatchesStoreID should confirm the resolved path still contains this store")
	}
}

// TestNewLayout_InventoryPrunesStaleEntries verifies stale inventory entries get pruned
func TestNewLayout_InventoryPrunesStaleEntries(t *testing.T) {
	haulerDir := t.TempDir()

	type entry struct {
		Path    string `json:"path"`
		Updated string `json:"updated"`
	}
	staleID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	inv := map[string]entry{
		staleID: {Path: filepath.Join(haulerDir, "gone"), Updated: "2026-01-01T00:00:00Z"},
	}
	data, err := json.Marshal(inv)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	invPath := filepath.Join(haulerDir, consts.DefaultStoreInventoryName)
	if err := os.WriteFile(invPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	storeDir := t.TempDir()
	if _, err := store.NewLayout(storeDir, store.WithHaulerDir(haulerDir)); err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	if _, _, err := store.ResolveStoreID(haulerDir, staleID); err == nil {
		t.Error("expected stale inventory entry to be pruned, but it still resolved")
	}
}
