package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// seedStoreMetadata writes a store.json at dir claiming storeID, so MatchesStoreID (and updateStoreInventory's own pruning) treats dir as a real store.
func seedStoreMetadata(t *testing.T, dir, storeID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}
	data, err := json.Marshal(storeMetadata{StoreID: storeID})
	if err != nil {
		t.Fatalf("failed to marshal store metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store.json"), data, 0o644); err != nil {
		t.Fatalf("failed to write store.json: %v", err)
	}
}

// TestUpdateStoreInventory_ConcurrentWritesDontLoseEntries is a regression test for a lost-update race where concurrent hauler processes sharing a haulerDir could silently clobber each other's inventory entry.
func TestUpdateStoreInventory_ConcurrentWritesDontLoseEntries(t *testing.T) {
	haulerDir := t.TempDir()

	const n = 20
	storeIDs := make([]string, n)
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		storeIDs[i] = "store-" + string(rune('a'+i))
		paths[i] = filepath.Join(t.TempDir(), storeIDs[i])
		seedStoreMetadata(t, paths[i], storeIDs[i])
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id, path string) {
			defer wg.Done()
			updateStoreInventory(haulerDir, id, path)
		}(storeIDs[i], paths[i])
	}
	wg.Wait()

	inv := loadInventory(haulerDir)
	if len(inv) != n {
		t.Fatalf("expected %d inventory entries after concurrent writes, got %d: %v", n, len(inv), inv)
	}
	for i, id := range storeIDs {
		entry, ok := inv[id]
		if !ok {
			t.Errorf("entry for %s is missing after concurrent writes", id)
			continue
		}
		if entry.Path != paths[i] {
			t.Errorf("entry for %s has path %q, want %q", id, entry.Path, paths[i])
		}
	}
}

// TestUpdateStoreInventory_ConcurrentFirstCreationSucceeds is a regression test for the mkdir race behind "failed to create hauler directory for store inventory": concurrent processes opening stores against a haulerDir that doesn't exist yet must all register successfully instead of losing the mkdir race.
func TestUpdateStoreInventory_ConcurrentFirstCreationSucceeds(t *testing.T) {
	haulerDir := filepath.Join(t.TempDir(), "not-yet-created", "hauler")

	const n = 20
	storeIDs := make([]string, n)
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		storeIDs[i] = "store-" + string(rune('a'+i))
		paths[i] = filepath.Join(t.TempDir(), storeIDs[i])
		seedStoreMetadata(t, paths[i], storeIDs[i])
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id, path string) {
			defer wg.Done()
			updateStoreInventory(haulerDir, id, path)
		}(storeIDs[i], paths[i])
	}
	wg.Wait()

	inv := loadInventory(haulerDir)
	if len(inv) != n {
		t.Fatalf("expected %d inventory entries after concurrent first creation, got %d: %v", n, len(inv), inv)
	}
}

// TestEnsureDir_ConcurrentFirstCreationSucceeds mirrors pkg/audit's copy of this test: many callers racing to create the same not-yet-existing directory must all succeed.
func TestEnsureDir_ConcurrentFirstCreationSucceeds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared", "dir")

	const n = 50
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ensureDir(dir)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: ensureDir returned error: %v", i, err)
		}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("expected %s to exist as a directory, stat err: %v", dir, err)
	}
}
