package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestInventoryButtonCellsTakeNoItem(t *testing.T) {
	var one uint16 // an item that covers a single cell
	for id := uint16(41); one == 0; id++ {
		if w, h := itemFootprint(id); w == 1 && h == 1 {
			one = id
		}
	}
	var inv [64]wire.Item
	if !carryFits(&inv, 59, one) {
		t.Error("slot 59 must be usable")
	}
	for _, s := range []int{60, 61, 62, 63} {
		if carryFits(&inv, s, one) {
			t.Errorf("slot %d must not receive items", s)
		}
	}
	for i := range invUsable {
		inv[i] = wire.Item{Index: one}
	}
	inv[59] = wire.Item{}
	if got := firstCarrySlot(&inv, one); got != 59 {
		t.Errorf("first free slot %d, want 59", got)
	}
	inv[59] = wire.Item{Index: one}
	if got := firstCarrySlot(&inv, one); got != -1 {
		t.Errorf("full inventory gave slot %d", got)
	}
}

func TestBagsAreRemovedFromTheInventory(t *testing.T) {
	var pl wire.Mob
	pl.Inventory[60] = wire.Item{Index: itemBag}
	pl.Inventory[61] = wire.Item{Index: itemBag}
	pl.Inventory[62] = wire.Item{Index: 412}
	clearBagSlots(&pl)
	if pl.Inventory[60].Index != 0 || pl.Inventory[61].Index != 0 {
		t.Fatal("bags still in the inventory")
	}
	if pl.Inventory[62].Index != 412 {
		t.Fatal("a regular item was removed")
	}
	pl.Inventory[60] = wire.Item{Index: 1103}
	clearBagSlots(&pl)
	if pl.Inventory[60].Index != 1103 {
		t.Fatal("something else on slot 60 must stay")
	}
}

func TestStorageRespectsFootprints(t *testing.T) {
	st := make([]wire.Item, 168)
	st[0] = wire.Item{Index: gridItem2x2}
	for _, slot := range []int{1, 9, 10} {
		if storageFits(st, slot, gridItem2x2) {
			t.Errorf("slot %d is covered by the item at slot 0", slot)
		}
	}
	if storageFits(st, storageVisible, 412) {
		t.Error("slots the client does not show must not receive items")
	}
	if got := firstStorageSlot(st, gridItem2x2); got != 2 {
		t.Errorf("first free slot %d, want 2", got)
	}
}

func TestMoveIntoStorageChecksFootprint(t *testing.T) {
	var pl wire.Mob
	st := make([]wire.Item, 168)
	st[4] = wire.Item{Index: gridItem2x2}
	moving := wire.Item{Index: gridItem2x2}
	if carryMoveFits(&pl, st, wire.SlotInv, 0, wire.SlotStorage, 5, &moving, &wire.Item{}) {
		t.Error("the destination overlaps the item at cargo slot 4")
	}
	if !carryMoveFits(&pl, st, wire.SlotInv, 0, wire.SlotStorage, 6, &moving, &wire.Item{}) {
		t.Error("the destination is free")
	}
}

func TestSettleStorageKeepsEveryItem(t *testing.T) {
	var st [168]wire.Item
	st[0] = wire.Item{Index: gridItem2x2}
	st[1] = wire.Item{Index: gridItem2x2}
	st[150] = wire.Item{Index: 412}
	settleStorage(&st)
	if st[0].Index != gridItem2x2 || st[1].Index != 0 || st[2].Index != gridItem2x2 {
		t.Fatalf("overlap not removed: %+v", st[:3])
	}
	if st[150].Index != 0 || firstSlotOfStorage(&st, 412) >= storageVisible {
		t.Fatal("the hidden item was not brought back")
	}

	var full [168]wire.Item
	for i := range 160 {
		full[i] = wire.Item{Index: gridItem2x2}
	}
	settleStorage(&full)
	count := 0
	for i := range full {
		if full[i].Index != 0 {
			count++
		}
	}
	if count != 160 {
		t.Fatalf("%d items left of 160", count)
	}
}

func firstSlotOfStorage(st *[168]wire.Item, id uint16) int {
	for i := range st {
		if st[i].Index == id {
			return i
		}
	}
	return -1
}
