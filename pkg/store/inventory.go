package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// inventoryEntry is a store's last-known location, keyed by StoreID
type inventoryEntry struct {
	Path    string `json:"path"`
	Updated string `json:"updated"`
}

type storeInventory map[string]inventoryEntry

func inventoryPath(haulerDir string) string {
	return filepath.Join(haulerDir, consts.DefaultStoreInventoryName)
}

func loadInventory(haulerDir string) storeInventory {
	inv := storeInventory{}
	p := inventoryPath(haulerDir)
	data, err := os.ReadFile(p)
	if err != nil {
		return inv
	}
	if err := json.Unmarshal(data, &inv); err != nil {
		zlog.Warn().Err(err).Str("path", p).Msg("failed to parse store inventory... ignoring")
		return storeInventory{}
	}
	return inv
}

func saveInventory(haulerDir string, inv storeInventory) error {
	if err := os.MkdirAll(haulerDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	// write to a temp file and rename to avoid a partially-written inventory
	tmp := inventoryPath(haulerDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, inventoryPath(haulerDir))
}

// updateStoreInventory records storeID's path in <haulerDir>/stores.json and
// prunes any other entries that no longer contain the store they claim
func updateStoreInventory(haulerDir, storeID, path string) {
	inv := loadInventory(haulerDir)

	for id, entry := range inv {
		if id == storeID {
			continue
		}
		if !MatchesStoreID(entry.Path, id) {
			delete(inv, id)
		}
	}

	inv[storeID] = inventoryEntry{
		Path:    path,
		Updated: time.Now().UTC().Format(time.RFC3339),
	}

	if err := saveInventory(haulerDir, inv); err != nil {
		zlog.Warn().Err(err).Msg("failed to update store inventory... store id lookup may not find this store later")
	}
}

// MatchesStoreID reports whether path's store.json identifies it as storeID
func MatchesStoreID(path, storeID string) bool {
	data, err := os.ReadFile(filepath.Join(path, consts.DefaultStoreMetadataName))
	if err != nil {
		return false
	}
	var m storeMetadata
	if json.Unmarshal(data, &m) != nil {
		return false
	}
	return m.StoreID == storeID
}

// ResolveStoreID looks up idOrPrefix (a StoreID or an unambiguous prefix of
// one) in <haulerDir>/stores.json and returns the matched store's full id and
// last-known path. Callers should verify the path with MatchesStoreID before
// trusting it
func ResolveStoreID(haulerDir, idOrPrefix string) (id string, path string, err error) {
	inv := loadInventory(haulerDir)

	if entry, ok := inv[idOrPrefix]; ok {
		return idOrPrefix, entry.Path, nil
	}

	var matchID, matchPath string
	count := 0
	for invID, entry := range inv {
		if strings.HasPrefix(invID, idOrPrefix) {
			matchID, matchPath = invID, entry.Path
			count++
		}
	}

	switch count {
	case 0:
		return "", "", fmt.Errorf("no store found matching id %q", idOrPrefix)
	case 1:
		return matchID, matchPath, nil
	default:
		return "", "", fmt.Errorf("store id %q is ambiguous and matches multiple stores", idOrPrefix)
	}
}
