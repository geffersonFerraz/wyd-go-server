package game

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/gob"
	"encoding/json"
	"os"
	"sync"
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
	gd.Generators = GeneratorsFixture(t)
	return gd
}

// GeneratorsFixture is the generators of the old config file, which also
// fills an empty gamedata.npc_generators.
func GeneratorsFixture(t *testing.T) []store.Generator {
	t.Helper()
	gens, err := ReadGeneratorsFile(ConfigDir + "/" + GeneratorsFile)
	if err != nil {
		t.Fatal(err)
	}
	return gens
}

// ConfigDir is the real config folder, seen from this package's tests.
const ConfigDir = "../../config"

var (
	worldOnce sync.Once
	world     *store.WorldAssets
	worldErr  error
)

// WorldFixture is the binary world (maps, ItemList.bin, templates) as it was
// when the source files went into the database.
func WorldFixture(t *testing.T) *store.WorldAssets {
	t.Helper()
	worldOnce.Do(func() {
		f, err := os.Open("testdata/world.gob.gz")
		if err != nil {
			worldErr = err
			return
		}
		defer f.Close()
		z, err := gzip.NewReader(f)
		if err != nil {
			worldErr = err
			return
		}
		world = &store.WorldAssets{}
		worldErr = gob.NewDecoder(z).Decode(world)
	})
	if worldErr != nil {
		t.Fatal(worldErr)
	}
	return world
}

// RealSettings is the repository's tmsrv.yaml.
func RealSettings(t *testing.T) Settings {
	t.Helper()
	s, err := LoadSettings("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// LoadRealData loads the real world the way a TMSRV does.
func LoadRealData(t *testing.T) *Data {
	t.Helper()
	d, err := LoadData(ConfigDir, RealSettings(t), WorldFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyGenerators(GeneratorsFixture(t)); err != nil {
		t.Fatal(err)
	}
	return d
}

// memLocks is a LockStore in memory.
type memLocks map[string][]byte

func (m memLocks) AccountLock(_ context.Context, u string) ([]byte, error) { return m[u], nil }
func (m memLocks) SetAccountLock(_ context.Context, u string, b []byte) error {
	m[u] = bytes.Clone(b)
	return nil
}
func (m memLocks) DeleteAccountLock(_ context.Context, u string) error {
	delete(m, u)
	return nil
}
