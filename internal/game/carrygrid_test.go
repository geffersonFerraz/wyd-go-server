package game

import (
	"testing"

	"dbsrv/internal/wire"
)

const (
	gridItem2x2 = 1103 // size 5 in itemgrid.bin
)

func TestCarryFootprintBlocksCoveredCells(t *testing.T) {
	var inv [64]wire.Item
	inv[0] = wire.Item{Index: gridItem2x2}
	for _, slot := range []int{1, 9, 10} {
		if carryFits(&inv, slot, gridItem2x2) {
			t.Errorf("slot %d is covered by the item at slot 0", slot)
		}
	}
	if !carryFits(&inv, 2, gridItem2x2) {
		t.Error("slot 2 is free")
	}
}

func TestCarryFitsRespectsGridEdges(t *testing.T) {
	var inv [64]wire.Item
	if carryFits(&inv, 8, gridItem2x2) {
		t.Error("a 2-wide item cannot start in the last column")
	}
	if carryFits(&inv, 54, gridItem2x2) {
		t.Error("a 2-tall item cannot start in the last row")
	}
}

func TestCarrySlotForMovesToAFittingSlot(t *testing.T) {
	var inv [64]wire.Item
	inv[0] = wire.Item{Index: gridItem2x2}
	if got := carrySlotFor(&inv, gridItem2x2, 1); got != 2 {
		t.Errorf("requested a covered slot, got %d", got)
	}
	if got := carrySlotFor(&inv, gridItem2x2, 4); got != 4 {
		t.Errorf("a free slot must be kept, got %d", got)
	}
}

func TestCarryMoveFitsChecksBothItemsOfASwap(t *testing.T) {
	var pl wire.Mob
	pl.Inventory[4] = wire.Item{Index: gridItem2x2}
	moving := wire.Item{Index: gridItem2x2}
	if carryMoveFits(&pl, nil, wire.SlotInv, 20, wire.SlotInv, 5, &moving, &wire.Item{}) {
		t.Error("the destination overlaps the item at slot 4")
	}
	if !carryMoveFits(&pl, nil, wire.SlotInv, 20, wire.SlotInv, 6, &moving, &wire.Item{}) {
		t.Error("the destination is free")
	}
	back := wire.Item{Index: gridItem2x2}
	if carryMoveFits(&pl, nil, wire.SlotInv, 3, wire.SlotInv, 30, &moving, &back) {
		t.Error("the swapped item would overlap slot 4 from slot 3")
	}
	if pl.Inventory[30].Index != 0 {
		t.Error("the check must leave the inventory untouched")
	}
}
