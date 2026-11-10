package game

import (
	"sort"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The three buttons on the last cells of the inventory grid (new, no C++
// equivalent). The client sends a MSG_STANDARDPARM; the server changes the
// inventory and resends it whole (0x185).

const opCliInvAction = 0xF40

const (
	invActionDestroyAll = 1
	invActionSortAsc    = 2
	invActionSortDesc   = 3
	invActionDropAll    = 4
)

func (g *Game) requestInvAction(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if u.status != userPlay || pl.Status.CurHP <= 0 {
		g.sendHpMode(u)
		return
	}
	g.cancelTrade(u)
	switch p.Value {
	case invActionDestroyAll:
		if g.locked(u, lockTrade, "Bloqueio de troca ativo!") {
			return
		}
		g.destroyAllItems(u)
	case invActionSortAsc, invActionSortDesc:
		g.sortInventory(u, p.Value == invActionSortDesc)
	case invActionDropAll:
		if u.accessLevel == 0 && g.locked(u, lockTrade, "Bloqueio de troca ativo!") {
			return
		}
		g.dropAllItems(u)
	default:
		return
	}
	g.sendCarry(u)
	g.saveUser(u, 0)
}

// destroyAllItems is the inventory bin for every slot; what the bin refuses
// (509) stays.
func (g *Game) destroyAllItems(u *User) {
	pl := &g.mobs[u.id].info.Player
	n := 0
	for i := range invUsable {
		it := &pl.Inventory[i]
		if it.Index == 0 || it.Index == 509 {
			continue
		}
		g.log.Info("item deleted (destroy all)", "slot", u.id, "inv", i, "item", it.Index, "effects", it.Effect)
		*it = wire.Item{}
		n++
	}
	g.clientMessage(u, "%d itens destruídos.", n)
}

// dropAllItems puts every item a player may drop on the ground around them,
// as RequestDropItem would, one cell each.
func (g *Game) dropAllItems(u *User) {
	pl := &g.mobs[u.id].info.Player
	t := g.mobs[u.id].target
	dropped, kept := 0, 0
	for i := range invUsable {
		it := &pl.Inventory[i]
		if it.Index == 0 || int(it.Index) >= MaxItemList {
			continue
		}
		if guildMedal(it.Index) || (u.accessLevel == 0 && g.playerCantDrop(it)) {
			kept++
			continue
		}
		gid := g.dropNear(int(t.X), int(t.Y), *it, g.charName(u.id))
		if gid <= 0 {
			kept++
			break // no room left on the ground
		}
		if r := g.takeTrack(u, wire.SlotInv, i); r != nil {
			g.ground[gid].track, g.ground[gid].trackFrom = r, g.partyOf(u)
			g.leaveTrack(u, r)
		}
		g.log.Info("dropped item (drop all)", "slot", u.id, "item", it.Index, "effects", it.Effect,
			"x", g.ground[gid].x, "y", g.ground[gid].y, "inv_slot", i)
		*it = wire.Item{}
		dropped++
	}
	if kept > 0 {
		g.clientMessage(u, "%d itens largados; %d ficaram no inventário.", dropped, kept)
	}
}

// sortInventory joins the stacks of the same item and lays the inventory out
// again ordered by item index. Items that cover several cells keep the order
// when they fit; otherwise the biggest go first. Nothing changes when the
// new layout cannot hold every item.
func (g *Game) sortInventory(u *User, desc bool) {
	pl := &g.mobs[u.id].info.Player
	type entry struct {
		it    wire.Item
		track *trackRef
	}
	var items []entry
	for i := range invSlots {
		if pl.Inventory[i].Index == 0 {
			continue
		}
		items = append(items, entry{pl.Inventory[i], g.takeTrack(u, wire.SlotInv, i)})
	}
	restore := func() {
		for _, e := range items {
			if e.track != nil {
				g.giveTrack(u, e.track, -1, -1)
			}
		}
		g.reconcileTracks(u)
	}

	// Join stacks (tracked items are never stackables).
	for a := range items {
		for b := a + 1; b < len(items) && items[a].it.Index != 0; b++ {
			if items[b].it.Index == items[a].it.Index && maxAmount[items[a].it.Index] > 0 {
				agroupItem(&items[b].it, &items[a].it)
			}
		}
	}
	kept := items[:0]
	for _, e := range items {
		if e.it.Index != 0 {
			kept = append(kept, e)
		}
	}
	items = kept

	less := func(a, b wire.Item) bool {
		if a.Index != b.Index {
			if desc {
				return a.Index > b.Index
			}
			return a.Index < b.Index
		}
		return itemAmount(&a) > itemAmount(&b)
	}
	sort.SliceStable(items, func(a, b int) bool { return less(items[a].it, items[b].it) })

	place := func(order []entry) ([invSlots]wire.Item, []int, bool) {
		var grid [invSlots]wire.Item
		slots := make([]int, len(order))
		for i, e := range order {
			s := firstGridSlot(grid[:], e.it.Index)
			if s < 0 {
				return grid, nil, false
			}
			grid[s] = e.it
			slots[i] = s
		}
		return grid, slots, true
	}
	grid, slots, ok := place(items)
	if !ok {
		area := func(it wire.Item) int { w, h := itemFootprint(it.Index); return w * h }
		sort.SliceStable(items, func(a, b int) bool { return area(items[a].it) > area(items[b].it) })
		grid, slots, ok = place(items)
	}
	if !ok {
		restore()
		g.clientMessage(u, "Não foi possível organizar o inventário.")
		return
	}
	copy(pl.Inventory[:invSlots], grid[:])
	for i, e := range items {
		if e.track != nil {
			g.giveTrack(u, e.track, wire.SlotInv, slots[i])
		}
	}
}
