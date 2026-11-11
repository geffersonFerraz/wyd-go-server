package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func invAction(g *Game, u *User, action int32) {
	g.requestInvAction(u, packet(&wire.SignalValue{Header: wire.NewHeader(opCliInvAction, uint16(u.id)), Value: action}))
}

func TestInvActionSort(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	pl.Inventory[10] = dropStack(413, 3)
	pl.Inventory[20] = dropStack(412, 2)
	pl.Inventory[30] = dropStack(412, 5)
	pl.Inventory[40] = wire.Item{Index: 1103} // 2x2

	invAction(g, u, invActionSortAsc)
	if pl.Inventory[0].Index != 412 || itemAmount(&pl.Inventory[0]) != 7 || pl.Inventory[1].Index != 413 || pl.Inventory[2].Index != 1103 {
		t.Fatalf("asc: %v %v %v", pl.Inventory[0], pl.Inventory[1], pl.Inventory[2])
	}
	for i := 3; i < invSlots; i++ {
		if pl.Inventory[i].Index != 0 {
			t.Fatalf("asc: leftover at %d: %v", i, pl.Inventory[i])
		}
	}

	// A second sort within the cooldown does nothing.
	invAction(g, u, invActionSortDesc)
	if pl.Inventory[0].Index != 412 {
		t.Fatalf("sorted again within the cooldown: %v", pl.Inventory[0])
	}

	u.lastInvSort = time.Now().Add(-invSortCooldown)
	invAction(g, u, invActionSortDesc)
	if pl.Inventory[0].Index != 1103 || pl.Inventory[2].Index != 413 || pl.Inventory[3].Index != 412 {
		t.Fatalf("desc: %v %v %v %v", pl.Inventory[0], pl.Inventory[1], pl.Inventory[2], pl.Inventory[3])
	}
}

func TestInvActionDestroyAll(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	pl.Inventory[0] = wire.Item{Index: 412}
	pl.Inventory[5] = wire.Item{Index: 509}
	pl.Inventory[63] = wire.Item{Index: 7} // PK counter slot stays
	invAction(g, u, invActionDestroyAll)
	if pl.Inventory[0].Index != 0 || pl.Inventory[5].Index != 509 || pl.Inventory[63].Index != 7 {
		t.Fatalf("destroy all: %v %v %v", pl.Inventory[0], pl.Inventory[5], pl.Inventory[63])
	}
}

func TestInvActionDropAll(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	pl.Inventory[0] = wire.Item{Index: 412}
	pl.Inventory[1] = wire.Item{Index: 413}
	pl.Inventory[2] = wire.Item{Index: 747} // players cannot drop it
	invAction(g, u, invActionDropAll)
	if pl.Inventory[0].Index != 0 || pl.Inventory[1].Index != 0 || pl.Inventory[2].Index != 747 {
		t.Fatalf("drop all: %v %v %v", pl.Inventory[0], pl.Inventory[1], pl.Inventory[2])
	}
	found := 0
	for i := g.initCount + 1; i < maxGroundItem; i++ {
		if gi := g.ground[i]; gi.open && (gi.item.Index == 412 || gi.item.Index == 413) {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("%d items on the ground, want 2", found)
	}
}
