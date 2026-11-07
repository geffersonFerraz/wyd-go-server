package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/legacy"
	"dbsrv/internal/wire"
)

var testLoc = time.FixedZone("BRT", -3*3600)

// sampleAccount returns a fully populated account: ANDERSON.xml if the legacy
// data is present, plus fields the XML never stored, so every part of the
// split/merge mapping is exercised.
func sampleAccount(t *testing.T) *AccountRecord {
	t.Helper()
	rec := &AccountRecord{Status: auth.StatusBlocked, PasswordHash: "$argon2id$x", NumericPassHash: "$argon2id$y"}
	path := "../../../Release Servidor/DBSRV/account/A/ANDERSON.xml"
	if _, err := os.Stat(path); err == nil {
		acc, err := legacy.ReadAccountFile(filepath.Clean(path))
		if err != nil {
			t.Fatal(err)
		}
		rec.Account = acc.Wire
	} else {
		wire.PutCString(rec.Account.Username[:], "ANDERSON")
		wire.PutCString(rec.Account.Mob[0].Player.Name[:], "DragonBlue")
		rec.Account.Mob[0].Player.Equip[0] = wire.Item{Index: 1}
	}
	a := &rec.Account
	a.Ban = wire.ItemDate{Dia: 5, Mes: 3, Ano: 2026, Hora: 21, Minuto: 7, Segundo: 9}
	a.BanType = 2
	wire.PutGameString(a.PassBlock[:], "blk")
	wire.PutGameString(a.Friends[3][:], "Amigão")
	a.Daily.Received[4] = true
	a.Daily.WeekYear = 44
	a.Unique = 0x8000000000000005 // > int64 max: must survive jsonb
	a.UniqueNovato = true
	a.LastPenalty = 1767225600
	a.StorageItems[167] = wire.Item{Effect: [3]wire.ItemEffect{{Index: 43}}} // index 0 but not empty
	m := &a.Mob[2]
	wire.PutGameString(m.Player.Name[:], "Ação")
	m.Player.Equip[0] = wire.Item{Index: 21, Effect: [3]wire.ItemEffect{{}, {Index: 21, Value: 2}, {}}}
	m.Player.BStatus.Level = 399
	m.Counters.PvPKill = 7
	m.DailyQuest.MobCount[4] = 99
	m.FilterDrop.IsEnabled = true
	m.FilterDrop.Items[23] = wire.Item{Index: 412}
	m.Sub.Equip[1] = wire.Item{Index: 3500}
	m.Fame = 1234
	m.Citizen = 3
	return rec
}

func TestSplitMergeRoundTrip(t *testing.T) {
	in := sampleAccount(t)

	row, chars, items, err := splitAccount(in, testLoc)
	if err != nil {
		t.Fatal(err)
	}
	if row.Username != "ANDERSON" || row.BanUntil == nil || row.BanUntil.Year() != 2026 {
		t.Errorf("columns wrong: %+v", row)
	}
	for _, c := range chars {
		if c.Slot == 2 && (c.Name != "Ação" || c.Level != 399 || c.Evolution != 2 || c.Face != 21) {
			t.Errorf("char columns wrong: %+v", c)
		}
		if bytes.Contains(c.Data, []byte(`"Inventory"`)) || bytes.Contains(c.Data, []byte(`"Equip":[{"Index"`)) {
			t.Errorf("items leaked into character json: %s", c.Data)
		}
	}
	if bytes.Contains(row.Data, []byte(`"Password"`)) || bytes.Contains(row.Data, []byte(`"Mob"`)) {
		t.Errorf("account json not stripped: %s", row.Data)
	}

	out, err := mergeAccount(42, row, chars, items, testLoc)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != 42 || out.Status != in.Status || out.PasswordHash != in.PasswordHash || out.NumericPassHash != in.NumericPassHash {
		t.Errorf("record meta: %+v", out)
	}

	// Compare byte images: catches any field the mapping drops.
	want, got := wire.MustMarshal(&in.Account), wire.MustMarshal(&out.Account)
	if !bytes.Equal(want, got) {
		for i := range want {
			if want[i] != got[i] {
				t.Fatalf("round trip differs first at STRUCT_ACCOUNT offset %d (%02x != %02x)", i, got[i], want[i])
			}
		}
	}
	t.Logf("account json %d bytes, %d characters, %d item rows", len(row.Data), len(chars), len(items))
}

func TestCompactJSONKeepsPositions(t *testing.T) {
	var c wire.CharInfo
	c.Affects[5] = wire.Affect{Index: 9}
	c.Sub.SkillBar[19] = 7
	raw, err := compactJSON(&c)
	if err != nil {
		t.Fatal(err)
	}
	var back wire.CharInfo
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back != c {
		t.Fatalf("compact json lost data: %s", raw)
	}
	if len(raw) > 200 {
		t.Errorf("compact json too large (%d bytes): %s", len(raw), raw)
	}
}
