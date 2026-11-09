package game

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"

	"dbsrv/internal/store"
)

// GameDataFixture is the game configuration the tests use: items, skills, drops
// and teleports as they were when the source files went into the database.
func GameDataFixture(t *testing.T) *store.GameData {
	t.Helper()
	f, err := os.Open("testdata/gamedata.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	gd := &store.GameData{}
	if err := json.NewDecoder(z).Decode(gd); err != nil {
		t.Fatal(err)
	}
	return gd
}
