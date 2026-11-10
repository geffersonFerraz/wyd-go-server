package wire

import (
	"bytes"
	"os"
	"testing"
)

// testdata/npc/* are the class templates of the C++ server, in the 32-bit
// layout of STRUCT_MOB. Decoding them checks real MSVC-written bytes against
// legacyMob and its conversion to the 64-bit Mob.
func TestDecodeNPCBaseFiles(t *testing.T) {
	want := []struct {
		file string
		face uint16
	}{
		{"TK", 1}, {"FM", 11}, {"BM", 21}, {"HT", 31},
	}
	for _, w := range want {
		raw, err := os.ReadFile("testdata/npc/" + w.file)
		if err != nil {
			t.Fatal(err)
		}
		var m Mob
		if err := UnmarshalLegacyMob(raw, &m); err != nil {
			t.Fatal(err)
		}
		if got := CString(m.Name[:]); got != w.file {
			t.Errorf("%s: name %q", w.file, got)
		}
		if m.Equip[0].Index != w.face {
			t.Errorf("%s: face %d, want %d", w.file, m.Equip[0].Index, w.face)
		}
		if m.Equip[0].Effect[1].Value != 1 {
			t.Errorf("%s: evolution (Equip[0].EFV2) %d, want 1 (mortal)", w.file, m.Equip[0].Effect[1].Value)
		}
		if m.Last != (Position{2096, 2096}) {
			t.Errorf("%s: last position %+v", w.file, m.Last)
		}
		if m.BStatus.Move != 82 || m.BStatus.MaxHP == 0 || m.BStatus.MaxHP != m.BStatus.CurHP {
			t.Errorf("%s: base status looks misaligned: %+v", w.file, m.BStatus)
		}
		if m.Equip[1].Index < 800 || m.Equip[1].Index > 2000 {
			t.Errorf("%s: Equip[1] index %d is not a starting item", w.file, m.Equip[1].Index)
		}
		// Template data is not perfectly clean (BM has a stray EF1=43 at
		// Inventory[12]), so only check the first slots.
		for i, it := range m.Inventory[:8] {
			if it != (Item{}) {
				t.Errorf("%s: inventory[%d] = %+v, templates start empty", w.file, i, it)
			}
		}

		var l legacyMob
		if err := Unmarshal(raw, &l); err != nil {
			t.Fatal(err)
		}
		back := MustMarshal(&l)
		// Bytes we model as padding are zeroed on write; templates have none
		// set, so the round trip must be exact.
		if !bytes.Equal(back, raw) {
			for i := range raw {
				if back[i] != raw[i] {
					t.Errorf("%s: round trip differs first at byte %d (%02x vs %02x)", w.file, i, back[i], raw[i])
					break
				}
			}
		}
	}
}

func TestUnmarshalShortTMSRVSave(t *testing.T) {
	var in SaveCharacter
	in.Header = NewHeader(OpSaveMob, 7)
	PutCString(in.User[:], "ANDERSON")
	PutCString(in.PassBlock[:], "1234")
	in.Cash = 13300
	full := MustMarshal(&in)

	// TMSRV sends only 5872 bytes (no TypeBlock).
	var out SaveCharacter
	if err := Unmarshal(full[:5872], &out); err != nil {
		t.Fatal(err)
	}
	if CString(out.User[:]) != "ANDERSON" || out.Cash != 13300 || CString(out.PassBlock[:]) != "1234" {
		t.Errorf("short decode lost fields: user=%q cash=%d pb=%q", CString(out.User[:]), out.Cash, CString(out.PassBlock[:]))
	}
}

func TestCStrings(t *testing.T) {
	var b [16]byte
	PutCString(b[:], "0123456789ABCDEFGHIJ")
	if got := CString(b[:]); got != "0123456789ABCDE" {
		t.Errorf("truncation: %q", got)
	}
	if b[15] != 0 {
		t.Error("not NUL terminated")
	}
	if s := NewMSVCString("ANGELS"); s.String() != "ANGELS" || s.Capacity != 15 {
		t.Errorf("msvc string %+v", s)
	}
}

func TestLegacyMobWidensValues(t *testing.T) {
	var l legacyMob
	l.Gold = 2_000_000_000
	l.Status.CurHP, l.BStatus.Attack = 150_000, 90_000
	l.Equip[3].Index = 777
	var m Mob
	if err := UnmarshalLegacyMob(MustMarshal(&l), &m); err != nil {
		t.Fatal(err)
	}
	if m.Gold != 2_000_000_000 || m.Status.CurHP != 150_000 || m.BStatus.Attack != 90_000 || m.Equip[3].Index != 777 {
		t.Errorf("%+v", m)
	}
}

func TestEffectValueIs16Bits(t *testing.T) {
	in := Item{Index: 412}
	in.Effect[1] = ItemEffect{Index: 61, Value: 40000}
	var out Item
	if err := Unmarshal(MustMarshal(&in), &out); err != nil || out != in {
		t.Fatalf("%v %+v", err, out)
	}
	var old [20]legacyItem
	old[3].Index, old[3].Effect[0].Index, old[3].Effect[0].Value = 805, 43, 200
	var eq [20]Item
	if err := UnmarshalEquip(MustMarshal(&old), &eq); err != nil || eq[3].Index != 805 || eq[3].Effect[0].Value != 200 {
		t.Fatalf("old row: %v %+v", err, eq[3])
	}
}
