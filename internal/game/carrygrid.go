package game

import (
	_ "embed"
	"sort"

	"dbsrv/internal/wire"
)

// itemGridSizes holds, per item index, the g_pItemGridXY entry of the 7.543
// ItemList: how many inventory cells the item covers. The 7.87 item list
// lost these values (every EF_GRID is 0).
//
//go:embed itemgrid.bin
var itemGridSizes []byte

const (
	carryColumns = 9
	carryRows    = 7
)

var itemGridXY = [8][2]int{{1, 1}, {1, 2}, {1, 3}, {1, 4}, {2, 1}, {2, 2}, {2, 3}, {2, 4}}

// itemFootprint returns how many columns and rows an item covers.
func itemFootprint(id uint16) (int, int) {
	size := 0
	if int(id) < len(itemGridSizes) {
		size = int(itemGridSizes[id])
	}
	if size >= len(itemGridXY) {
		size = 0
	}
	return itemGridXY[size][0], itemGridXY[size][1]
}

// carryOccupancy marks the inventory cells covered by the items in inv.
func carryOccupancy(inv *[64]wire.Item) (occupied [carryRows][carryColumns]bool) {
	for slot := range invSlots {
		if inv[slot].Index == 0 {
			continue
		}
		w, h := itemFootprint(inv[slot].Index)
		x, y := slot%carryColumns, slot/carryColumns
		for cy := y; cy < y+h && cy < carryRows; cy++ {
			for cx := x; cx < x+w && cx < carryColumns; cx++ {
				occupied[cy][cx] = true
			}
		}
	}
	return occupied
}

// settleCarry removes the overlaps of a saved inventory: items keep their slot
// in order while they fit, the others move to a free place (largest first).
// What finds no place is returned.
func settleCarry(inv *[64]wire.Item) (left []wire.Item) {
	var placed [64]wire.Item
	copy(placed[invSlots:], inv[invSlots:])
	var pending []wire.Item
	for slot := range invSlots {
		switch it := inv[slot]; {
		case it.Index == 0:
		case carryFits(&placed, slot, it.Index):
			placed[slot] = it
		default:
			pending = append(pending, it)
		}
	}
	area := func(it wire.Item) int {
		w, h := itemFootprint(it.Index)
		return w * h
	}
	sort.SliceStable(pending, func(a, b int) bool { return area(pending[a]) > area(pending[b]) })
	for _, it := range pending {
		if slot := firstCarrySlot(&placed, it.Index); slot >= 0 {
			placed[slot] = it
		} else {
			left = append(left, it)
		}
	}
	*inv = placed
	return left
}

// carryFits reports whether item id can be placed with its top-left cell at
// slot, without covering another item.
func carryFits(inv *[64]wire.Item, slot int, id uint16) bool {
	if slot < 0 || slot >= invSlots {
		return false
	}
	w, h := itemFootprint(id)
	x, y := slot%carryColumns, slot/carryColumns
	if x+w > carryColumns || y+h > carryRows {
		return false
	}
	occupied := carryOccupancy(inv)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			if cy*carryColumns+cx >= invSlots || occupied[cy][cx] {
				return false
			}
		}
	}
	return true
}

// firstCarrySlot is the first slot where item id fits, or -1.
func firstCarrySlot(inv *[64]wire.Item, id uint16) int {
	for slot := range invSlots {
		if inv[slot].Index == 0 && carryFits(inv, slot, id) {
			return slot
		}
	}
	return -1
}

// carrySlotFor keeps the requested slot when the item fits there and looks for
// another one otherwise; -1 means the inventory has no room for it.
func carrySlotFor(inv *[64]wire.Item, id uint16, requested int) int {
	if inv[requested].Index == 0 && carryFits(inv, requested, id) {
		return requested
	}
	return firstCarrySlot(inv, id)
}

// carryMoveFits validates a move once both slots were emptied: newSrc goes to
// the destination and newDst to the source.
func carryMoveFits(pl *wire.Mob, src, ss, dst, ds int, newSrc, newDst *wire.Item) bool {
	if dst == wire.SlotInv && newSrc.Index != 0 && !carryFits(&pl.Inventory, ds, newSrc.Index) {
		return false
	}
	if src != wire.SlotInv || newDst.Index == 0 {
		return true
	}
	if dst == wire.SlotInv {
		pl.Inventory[ds] = *newSrc
		defer func() { pl.Inventory[ds] = wire.Item{} }()
	}
	return carryFits(&pl.Inventory, ss, newDst.Index)
}
