package wire

import (
	"bytes"
	"testing"
)

func TestPatchLegacyMob(t *testing.T) {
	orig := make([]byte, LegacyMobSize)
	for i := range orig {
		orig[i] = byte(i*7 + 3) // every byte set, padding included
	}
	var m Mob
	if err := UnmarshalLegacyMob(orig, &m); err != nil {
		t.Fatal(err)
	}
	same, err := PatchLegacyMob(orig, &m)
	if err != nil || !bytes.Equal(same, orig) {
		t.Fatalf("an unchanged template must keep every byte (err %v)", err)
	}

	m.Exp, m.BStatus.MaxHP, m.Equip[3].Index, m.Inventory[63].Effect[2].Value = 123456789, 50000, 911, 200
	PutCString(m.Name[:], "Troll")
	out, err := PatchLegacyMob(orig, &m)
	if err != nil {
		t.Fatal(err)
	}
	var back Mob
	if err := UnmarshalLegacyMob(out, &back); err != nil {
		t.Fatal(err)
	}
	if back != m {
		t.Fatal("the patched image does not decode to the edited mob")
	}
	if out[22] != orig[22] || out[847] != orig[847] { // QuestInfo and the tail padding
		t.Fatal("untouched bytes changed")
	}

	m.BStatus.MaxHP = 1 << 40
	if _, err := PatchLegacyMob(orig, &m); err == nil {
		t.Fatal("a value above 32 bits must be refused")
	}
	// A short image is zero-extended.
	short, err := PatchLegacyMob(orig[:100], &back)
	if err != nil || len(short) != LegacyMobSize {
		t.Fatalf("short image: len %d err %v", len(short), err)
	}
}
