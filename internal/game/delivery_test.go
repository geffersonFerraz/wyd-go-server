package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestItemDeliveryFullStorage(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTER")
	pl := &g.mobs[1].info.Player
	for i := range u.account.StorageItems {
		u.account.StorageItems[i] = wire.Item{Index: 412}
	}
	deliver := func(id uint16) {
		p := wire.ItemDelivery{Header: wire.NewHeader(wire.OpItemDelivery, 1), Item: wire.Item{Index: id}}
		wire.PutCString(p.Username[:], "TESTER")
		g.itemDelivery(u, packet(&p))
	}

	// Storage full: the item goes to the inventory.
	deliver(1103)
	if firstSlotOf(pl, 1103) < 0 {
		t.Fatal("not put in the inventory with a full storage")
	}

	// Inventory full too: nothing changes here (the item goes back to DBSRV).
	for i := range 60 {
		pl.Inventory[i] = wire.Item{Index: 413}
	}
	before := pl.Inventory
	deliver(1104)
	if pl.Inventory != before {
		t.Fatal("inventory changed with everything full")
	}
}
