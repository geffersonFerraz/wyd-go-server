package acctedit

import (
	"encoding/binary"
	"testing"

	"dbsrv/internal/exptable"
	"dbsrv/internal/wire"
)

func account() *wire.Account {
	var a wire.Account
	wire.PutCString(a.Mob[0].Player.Name[:], "Hero")
	a.Mob[0].Player.Equip[0].Effect[1].Value = 1 // mortal
	a.Mob[0].Player.Inventory[3] = wire.Item{Index: 412}
	return &a
}

func TestValidate(t *testing.T) {
	bad := []Edit{
		{Op: "nope"}, {Op: OpGold, Value: -1}, {Op: OpGold, Value: MaxGold + 1}, {Op: OpLevel, Value: 0},
		{Op: OpLevel, Value: 401}, {Op: OpAccess, Value: 256}, {Op: OpGold, Char: 4},
		{Op: OpItem, Where: wire.SlotEquip, Slot: 0, Item: []int{1, 0, 0, 0, 0, 0, 0}}, // face
		{Op: OpItem, Where: wire.SlotInv, Slot: 63, Item: []int{1, 0, 0, 0, 0, 0, 0}},  // PK points
		{Op: OpItem, Where: wire.SlotStorage, Slot: 128, Item: []int{1, 0, 0, 0, 0, 0, 0}},
		{Op: OpItem, Where: wire.SlotInv, Slot: 1, Item: []int{1, 300, 0, 0, 0, 0, 0}},
		{Op: OpItem, Where: wire.SlotInv, Slot: 1, Item: []int{1}},
	}
	for _, e := range bad {
		if e.Validate() == "" {
			t.Errorf("accepted %+v", e)
		}
	}
	good := []Edit{{Op: OpGold, Value: MaxGold}, {Op: OpLevel, Value: 400}, {Op: OpAccess, Value: 1},
		{Op: OpCoin, Value: 5}, {Op: OpItem, Where: wire.SlotStorage, Slot: 127, Item: []int{0, 0, 0, 0, 0, 0, 0}}}
	for _, e := range good {
		if msg := e.Validate(); msg != "" {
			t.Errorf("refused %+v: %s", e, msg)
		}
	}
}

func TestApply(t *testing.T) {
	a := account()
	for _, e := range []Edit{
		{Op: OpGold, Value: 777},
		{Op: OpLevel, Value: 100},
		{Op: OpAccess, Value: 3},
		{Op: OpCoin, Value: 9},
		{Op: OpItem, Where: wire.SlotEquip, Slot: 6, Item: []int{805, 43, 9, 0, 0, 0, 0}},
		{Op: OpItem, Where: wire.SlotStorage, Slot: 2, Item: []int{4140, 61, 10, 0, 0, 0, 0}},
		{Op: OpItem, Where: wire.SlotInv, Slot: 3, Item: []int{0, 0, 0, 0, 0, 0, 0}, Expect: []int{412, 0, 0, 0, 0, 0, 0}},
	} {
		if r := e.Apply(a); r != ResOK {
			t.Fatalf("%+v: %s", e, r)
		}
	}
	pl := &a.Mob[0].Player
	if pl.Gold != 777 || pl.BStatus.Level != 99 || pl.Exp != exptable.Next[1][98] || a.AccessLevel != 3 || a.StorageCoin != 9 {
		t.Fatalf("gold %d level %d exp %d access %d coin %d", pl.Gold, pl.BStatus.Level, pl.Exp, a.AccessLevel, a.StorageCoin)
	}
	if pl.Equip[6].Index != 805 || pl.Equip[6].Effect[0].Value != 9 || a.StorageItems[2].Index != 4140 || pl.Inventory[3].Index != 0 {
		t.Fatal("items not applied")
	}
	// A slot that changed since the admin looked is left alone.
	pl.Inventory[4] = wire.Item{Index: 413}
	e := Edit{Op: OpItem, Where: wire.SlotInv, Slot: 4, Item: []int{0, 0, 0, 0, 0, 0, 0}, Expect: []int{412, 0, 0, 0, 0, 0, 0}}
	if r := e.Apply(a); r != ResChanged || pl.Inventory[4].Index != 413 {
		t.Fatalf("changed slot: %s", r)
	}
	if r := (&Edit{Op: OpGold, Char: 2, Value: 1}).Apply(a); r != ResNoCharacter {
		t.Fatalf("missing character: %s", r)
	}
}

func TestPacketRoundTrip(t *testing.T) {
	if n := binary.Size(wire.AdminEdit{}); n != 112 {
		t.Fatalf("AdminEdit is %d bytes", n)
	}
	e := Edit{Op: OpItem, Char: 1, Where: wire.SlotInv, Slot: 7, Item: []int{412, 61, 3, 0, 0, 0, 0}, Expect: []int{0, 0, 0, 0, 0, 0, 0}}
	p := e.Packet("TESTER", 5)
	raw, err := wire.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	var back wire.AdminEdit
	if err := wire.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	got, err := FromPacket(&back)
	if err != nil {
		t.Fatal(err)
	}
	if got.Op != OpItem || got.Char != 1 || got.Slot != 7 || got.Item[0] != 412 || got.Item[2] != 3 || got.Expect == nil {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestMoveSwapsInventoryAndEquipment(t *testing.T) {
	var a wire.Account
	wire.PutCString(a.Mob[1].Player.Name[:], "X")
	pl := &a.Mob[1].Player
	pl.Inventory[4] = ItemOf([]int{805, 43, 300, 0, 0, 0, 0})
	pl.Equip[6] = ItemOf([]int{806, 0, 0, 0, 0, 0, 0})
	e := Edit{Op: OpMove, Char: 1, Slot: 4, To: 6, Expect: Ints(pl.Inventory[4]), ExpectTo: Ints(pl.Equip[6])}
	if msg := e.Validate(); msg != "" {
		t.Fatal(msg)
	}
	if r := e.Apply(&a); r != ResOK || pl.Equip[6].Index != 805 || pl.Equip[6].Effect[0].Value != 300 || pl.Inventory[4].Index != 806 {
		t.Fatalf("%q: %+v / %+v", r, pl.Equip[6], pl.Inventory[4])
	}
	if r := e.Apply(&a); r != ResChanged {
		t.Fatalf("second move: %q", r)
	}
	cape := e
	cape.To = 15
	if cape.Validate() == "" {
		t.Error("the cape slot must be refused")
	}
}

func TestRemoveIsAllOrNothing(t *testing.T) {
	var a wire.Account
	wire.PutCString(a.Mob[0].Player.Name[:], "X")
	pl := &a.Mob[0].Player
	pl.Inventory[1], pl.Equip[2] = ItemOf([]int{412, 0, 0, 0, 0, 0, 0}), ItemOf([]int{805, 0, 0, 0, 0, 0, 0})
	refs := []SlotRef{{Where: wire.SlotInv, Slot: 1, Expect: Ints(pl.Inventory[1])}, {Where: wire.SlotEquip, Slot: 2, Expect: []int{999, 0, 0, 0, 0, 0, 0}}}
	e := Edit{Op: OpRemove, Removes: refs}
	if msg := e.Validate(); msg != "" {
		t.Fatal(msg)
	}
	if r := e.Apply(&a); r != ResChanged || pl.Inventory[1].Index != 412 {
		t.Fatalf("changed slot: %q, inventory %+v", r, pl.Inventory[1])
	}
	refs[1].Expect = Ints(pl.Equip[2])
	if r := e.Apply(&a); r != ResOK || pl.Inventory[1].Index != 0 || pl.Equip[2].Index != 0 {
		t.Fatalf("remove: %q", r)
	}
	p := e.RemovePacket("ACC", 3)
	back, err := FromRemovePacket(&p)
	if err != nil || len(back.Removes) != 2 || back.Removes[1].Slot != 2 {
		t.Fatalf("packet: %v %+v", err, back)
	}
	for _, bad := range []Edit{
		{Op: OpRemove},
		{Op: OpRemove, Removes: []SlotRef{refs[0], refs[0]}},
		{Op: OpRemove, Removes: []SlotRef{refs[0], {Where: wire.SlotStorage, Slot: 1, Expect: refs[0].Expect}}},
		{Op: OpRemove, Removes: []SlotRef{{Where: wire.SlotEquip, Slot: 0, Expect: refs[0].Expect}}},
	} {
		if bad.Validate() == "" {
			t.Errorf("accepted %+v", bad)
		}
	}
}
