package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// dropScene has two players side by side, the second named differently.
func dropScene(t *testing.T) (*Game, *User, *User) {
	t.Helper()
	g := worldGame(t)
	a := placePlayer(g, 2112, 2042, 0)
	copy(g.mobs[1].info.Player.Name[:], "Dono")
	b := g.users[2]
	b.status = userPlay
	b.trade = emptyTrade()
	g.mobs[2].mode = 1
	g.mobs[2].info.Player.Status.CurHP = 100
	copy(g.mobs[2].info.Player.Name[:], "Outro")
	g.mobs[2].target = g.mobs[1].target
	return g, a, b
}

func dropFrom(g *Game, u *User, slot, x, y int) {
	g.requestDropItem(u, packet(&DropGroundItem{Header: wire.NewHeader(opCliDropItem, uint16(u.id)), InvType: wire.SlotInv, InvSlot: int32(slot), PosX: int16(x), PosY: int16(y)}))
}

func pickFrom(g *Game, u *User, id int) {
	gi := g.ground[id]
	g.requestPickItem(u, packet(&PickItem{Header: wire.NewHeader(opCliPickItem, uint16(u.id)), InvType: wire.SlotInv, InitID: int16(id + groundIDBase), PosX: int16(gi.x), PosY: int16(gi.y)}))
}

func groundIDOf(g *Game, index uint16) int {
	for i := g.initCount + 1; i < maxGroundItem; i++ {
		if g.ground[i].open && g.ground[i].item.Index == index {
			return i
		}
	}
	return 0
}

func TestDroppedItemIsReservedThenPublic(t *testing.T) {
	g, a, b := dropScene(t)
	pos := g.mobs[1].target
	g.mobs[1].info.Player.Inventory[4] = wire.Item{Index: 412}
	dropFrom(g, a, 4, int(pos.X)+1, int(pos.Y))
	id := groundIDOf(g, 412)
	if id == 0 || !g.ground[id].playerDrop {
		t.Fatalf("not on the ground as a player drop: id %d", id)
	}
	if got := g.ground[id].expireAt - g.ground[id].ownerUntil; got != dropLifetimeMs-dropOwnerMs {
		t.Fatalf("windows differ by %d ms", got)
	}

	pickFrom(g, b, id)
	if !g.ground[id].open || firstSlotOf(&g.mobs[2].info.Player, 412) >= 0 {
		t.Fatal("another player picked a reserved item")
	}

	g.ground[id].ownerUntil = g.now() - 1
	pickFrom(g, b, id)
	if g.ground[id].open || firstSlotOf(&g.mobs[2].info.Player, 412) < 0 {
		t.Fatal("an unreserved item could not be picked by anyone")
	}
}

func TestOwnerPicksDuringReservation(t *testing.T) {
	g, a, _ := dropScene(t)
	pos := g.mobs[1].target
	pl := &g.mobs[1].info.Player
	pl.Inventory[4] = wire.Item{Index: 412}
	dropFrom(g, a, 4, int(pos.X)+1, int(pos.Y))
	id := groundIDOf(g, 412)
	pickFrom(g, a, id)
	if g.ground[id].open || firstSlotOf(pl, 412) < 0 {
		t.Fatal("the owner could not pick their own item")
	}
}

func TestDropRefusals(t *testing.T) {
	g, a, _ := dropScene(t)
	pos := g.mobs[1].target
	pl := &g.mobs[1].info.Player

	pl.Inventory[0] = wire.Item{Index: 412}
	dropFrom(g, a, 0, int(pos.X)+dropMaxDistance+1, int(pos.Y))
	if pl.Inventory[0].Index == 0 {
		t.Fatal("dropped too far away")
	}

	pl.Inventory[0] = wire.Item{Index: 747} // guild item: not even tradable
	dropFrom(g, a, 0, int(pos.X)+1, int(pos.Y))
	if pl.Inventory[0].Index == 0 {
		t.Fatal("dropped a protected item")
	}

	pl.Equip[6] = wire.Item{Index: 1103}
	g.requestDropItem(a, packet(&DropGroundItem{Header: wire.NewHeader(opCliDropItem, 1), InvType: wire.SlotEquip, InvSlot: 6, PosX: int16(pos.X) + 1, PosY: int16(pos.Y)}))
	if pl.Equip[6].Index == 0 {
		t.Fatal("a player dropped equipment directly")
	}
}

func TestPutItemFallsOnTheGroundWithoutRoom(t *testing.T) {
	g, a, _ := dropScene(t)
	pl := &g.mobs[1].info.Player
	for slot := 0; slot+1 < invSlots; slot += 2 {
		if slot%carryColumns != carryColumns-1 && slot/carryColumns%2 == 0 {
			pl.Inventory[slot] = wire.Item{Index: gridItem2x2}
		}
	}
	for slot := range invSlots {
		if pl.Inventory[slot].Index == 0 && carryFits(&pl.Inventory, slot, 412) {
			pl.Inventory[slot] = wire.Item{Index: 412}
		}
	}
	before := pl.Inventory
	it := wire.Item{Index: gridItem2x2}
	if !g.putItem(a, &it) {
		t.Fatal("item lost")
	}
	if pl.Inventory != before {
		t.Fatal("the item was forced into the inventory")
	}
	id := groundIDOf(g, gridItem2x2)
	if id == 0 || g.ground[id].owner != "Dono" {
		t.Fatalf("not reserved on the ground: id %d", id)
	}
}

func TestNoTradeItemStaysReservedForItsWholeLife(t *testing.T) {
	g, a, _ := dropScene(t)
	it := wire.Item{Index: 412, Effect: [3]wire.ItemEffect{{Index: EF_NOTRADE, Value: 1}}}
	id := g.dropNearPlayer(a, it)
	if id == 0 || g.ground[id].ownerUntil != g.ground[id].expireAt {
		t.Fatalf("id %d: %+v", id, g.ground[id])
	}
}

func TestSettleCarryRemovesOverlaps(t *testing.T) {
	var inv [64]wire.Item
	inv[0] = wire.Item{Index: gridItem2x2}
	inv[1] = wire.Item{Index: gridItem2x2} // on the cells of slot 0
	inv[63] = wire.Item{Index: 547}
	if left := settleCarry(&inv); len(left) != 0 {
		t.Fatalf("left over: %+v", left)
	}
	if inv[0].Index != gridItem2x2 || inv[1].Index != 0 || inv[2].Index != gridItem2x2 {
		t.Fatalf("got %+v", inv[:3])
	}
	if inv[63].Index != 547 {
		t.Fatal("the slots outside the grid must stay")
	}
}

func TestSettleCarryReturnsWhatDoesNotFit(t *testing.T) {
	var inv [64]wire.Item
	for slot := range invSlots {
		inv[slot] = wire.Item{Index: gridItem2x2}
	}
	left := settleCarry(&inv)
	kept := 0
	for slot := range invSlots {
		if inv[slot].Index != 0 {
			kept++
		}
	}
	if kept == 0 || kept+len(left) != invSlots {
		t.Fatalf("kept %d, left %d", kept, len(left))
	}
}
