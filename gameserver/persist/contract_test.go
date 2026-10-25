package persist_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"hearth/gameserver/persist"
	"hearth/gameserver/persist/persisttest"
)

// TestJSONStoreContract runs the shared store contract against JSONStore — the
// reference every other store is measured against (see persisttest).
func TestJSONStoreContract(t *testing.T) {
	dir := t.TempDir()
	path := func(worldID string) string { return filepath.Join(dir, "world."+worldID+".save.json") }
	persisttest.Run(t, persisttest.Backend{
		Open: func(t *testing.T, worldID string) persist.Store {
			return persist.NewJSONStore(path(worldID))
		},
		ReadArchive: func(t *testing.T, _ persist.Store, worldID string, version int) *persist.Snapshot {
			b, err := os.ReadFile(fmt.Sprintf("%s.v%d.bak", path(worldID), version))
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				t.Fatal(err)
			}
			var snap persist.Snapshot
			if err := json.Unmarshal(b, &snap); err != nil {
				t.Fatal(err)
			}
			return &snap
		},
	})
}
