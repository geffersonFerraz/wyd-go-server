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
	carryColumns   = 9
	storageVisible = 126 // cargo slots the client shows: 9x14
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

// gridOccupancy marks the cells covered by the items of a 9-column grid.
func gridOccupancy(items []wire.Item) (occupied [storageVisible]bool) {
	rows := len(items) / carryColumns
	for slot := range items {
		if items[slot].Index == 0 {
			continue
		}
		w, h := itemFootprint(items[slot].Index)
		x, y := slot%carryColumns, slot/carryColumns
		for cy := y; cy < y+h && cy < rows; cy++ {
			for cx := x; cx < x+w && cx < carryColumns; cx++ {
				occupied[cy*carryColumns+cx] = true
			}
		}
	}
	return occupied
}

// gridFits reports whether item id can be placed with its top-left cell at
// slot of a 9-column grid, without covering another item.
func gridFits(items []wire.Item, slot int, id uint16) bool {
	if slot < 0 || slot >= len(items) {
		return false
	}
	w, h := itemFootprint(id)
	x, y := slot%carryColumns, slot/carryColumns
	if x+w > carryColumns || y+h > len(items)/carryColumns {
		return false
	}
	cells := len(items)
	if cells == invSlots {
		cells = invUsable // the inventory's last 3 cells take no item
	}
	occupied := gridOccupancy(items)
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			if c := cy*carryColumns + cx; c >= cells || occupied[c] {
				return false
			}
		}
	}
	return true
}

// firstGridSlot is the first slot where item id fits, or -1.
func firstGridSlot(items []wire.Item, id uint16) int {
	for slot := range items {
		if items[slot].Index == 0 && gridFits(items, slot, id) {
			return slot
		}
	}
	return -1
}

// settleGrid removes the overlaps of a saved grid: items keep their slot, in
// order, while they fit; the others move to a free place, largest first. What
// finds no place is returned.
func settleGrid(items []wire.Item) (left []wire.Item) {
	placed := make([]wire.Item, len(items))
	var pending []wire.Item
	for slot, it := range items {
		switch {
		case it.Index == 0:
		case gridFits(placed, slot, it.Index):
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
		if slot := firstGridSlot(placed, it.Index); slot >= 0 {
			placed[slot] = it
		} else {
			left = append(left, it)
		}
	}
	copy(items, placed)
	return left
}

// settleCarry removes the overlaps of a saved inventory.
func settleCarry(inv *[64]wire.Item) []wire.Item { return settleGrid(inv[:invSlots]) }

// settleStorage removes the overlaps of a saved cargo and brings back the items
// of the slots the client does not show. Unlike the inventory, nothing leaves
// the cargo: what geometrically does not fit stays on any free slot.
func settleStorage(st *[168]wire.Item) {
	visible, hidden := st[:storageVisible], st[storageVisible:160]
	var extras []wire.Item
	for i := range hidden {
		if hidden[i].Index != 0 {
			extras = append(extras, hidden[i])
			hidden[i] = wire.Item{}
		}
	}
	left := settleGrid(visible)
	for _, it := range extras {
		if slot := firstGridSlot(visible, it.Index); slot >= 0 {
			visible[slot] = it
		} else {
			left = append(left, it)
		}
	}
	free := append(visible[:len(visible):len(visible)], hidden...) // visible slots first
	for _, it := range left {
		for i := range free {
			if free[i].Index != 0 {
				continue
			}
			if i < storageVisible {
				visible[i] = it
			} else {
				hidden[i-storageVisible] = it
			}
			free[i] = it
			break
		}
	}
}

// carryFits reports whether item id fits with its top-left cell at slot.
func carryFits(inv *[64]wire.Item, slot int, id uint16) bool {
	return gridFits(inv[:invSlots], slot, id)
}

// firstCarrySlot is the first inventory slot where item id fits, or -1.
func firstCarrySlot(inv *[64]wire.Item, id uint16) int {
	return firstGridSlot(inv[:invSlots], id)
}

// storageFits is carryFits for the cargo.
func storageFits(st []wire.Item, slot int, id uint16) bool {
	return gridFits(st[:storageVisible], slot, id)
}

// firstStorageSlot is the first cargo slot where item id fits, or -1.
func firstStorageSlot(st []wire.Item, id uint16) int {
	return firstGridSlot(st[:storageVisible], id)
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
func carryMoveFits(pl *wire.Mob, storage []wire.Item, src, ss, dst, ds int, newSrc, newDst *wire.Item) bool {
	grid := func(typ int) []wire.Item {
		switch typ {
		case wire.SlotInv:
			return pl.Inventory[:invSlots]
		case wire.SlotStorage:
			return storage[:storageVisible]
		}
		return nil
	}
	dstGrid, srcGrid := grid(dst), grid(src)
	if dstGrid != nil && newSrc.Index != 0 && !gridFits(dstGrid, ds, newSrc.Index) {
		return false
	}
	if srcGrid == nil || newDst.Index == 0 {
		return true
	}
	if dstGrid != nil && ds < len(dstGrid) {
		dstGrid[ds] = *newSrc
		defer func() { dstGrid[ds] = wire.Item{} }()
	}
	return gridFits(srcGrid, ss, newDst.Index)
}
