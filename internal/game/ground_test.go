package game

import (
	"encoding/binary"
	"testing"

	"dbsrv/internal/wire"
)

func TestGroundPacketSizes(t *testing.T) {
	for _, c := range []struct {
		v    any
		size int
	}{
		{CreateGroundItem{}, 32}, {RemoveGroundItem{}, 16}, {GateState{}, 20},
		{PickItem{}, 28}, {DropGroundItem{}, 32}, {DropItemAck{}, 28},
		{AddPoint{}, 20}, {DeleteItem{}, 20}, {UngroupItem{}, 24},
		{MerchantClick{}, 20}, {PartyEvocation{}, 88},
	} {
		if got := binary.Size(c.v); got != c.size {
			t.Errorf("%T: %d bytes, want %d", c.v, got, c.size)
		}
	}
}

func TestInitItemsAndGates(t *testing.T) {
	g := worldGame(t)
	if len(g.data.InitItems) == 0 {
		t.Skip("no InitItem.csv")
	}
	if g.initCount != len(g.data.InitItems) {
		t.Fatalf("initCount %d, %d rows", g.initCount, len(g.data.InitItems))
	}
	for i, f := range g.data.InitItems {
		gi := &g.ground[i+1]
		if !gi.open || int(gi.item.Index) != f.Index || g.itemAt(gi.x, gi.y) != i+1 {
			t.Fatalf("row %d (%d at %d,%d) -> ground %+v", i, f.Index, f.X, f.Y, gi)
		}
	}

	// A keyed gate starts locked; find one and a key that opens it.
	gate, key := 0, 0
	for id := 1; id <= g.initCount && gate == 0; id++ {
		if k := g.data.itemAbility(&g.ground[id].item, EF_KEYID); k > 0 && k < 15 && g.ground[id].canRun >= 0 && g.ground[id].canRun < 6 && id != 24 {
			gate, key = id, k
		}
	}
	if gate == 0 {
		t.Skip("no keyed gate")
	}
	gi := &g.ground[gate]
	if gi.status != gateLocked {
		t.Fatalf("keyed gate %d status %d, want locked", gate, gi.status)
	}
	keyItem := uint16(0)
	for id := uint16(1); int(id) < MaxItemList; id++ {
		it := wire.Item{Index: id}
		if id != gi.item.Index && g.data.itemAbility(&it, EF_KEYID) == key {
			keyItem = id
			break
		}
	}
	// Remember the heights around the gate while locked.
	locked := make([]byte, 0, 36)
	for dy := -2; dy <= 3; dy++ {
		for dx := -2; dx <= 3; dx++ {
			locked = append(locked, g.data.Height[(gi.y+dy)*gridSize+gi.x+dx])
		}
	}

	u := placePlayer(g, gi.x+3, gi.y+3, 0)
	pl := &g.mobs[1].info.Player
	open := func() {
		g.requestOpenGate(u, packet(&GateState{Header: wire.NewHeader(opGate, 1), GateID: int32(gate + groundIDBase), Status: gateOpen}))
	}
	open()
	if gi.status != gateLocked {
		t.Fatal("opened without the key")
	}
	if keyItem == 0 {
		t.Skip("no key item for the gate")
	}
	pl.Inventory[5] = wire.Item{Index: keyItem}
	open()
	if gi.status != gateOpen || pl.Inventory[5].Index != 0 {
		t.Fatalf("with key: status %d, key slot %+v", gi.status, pl.Inventory[5])
	}
	changed := false
	i := 0
	for dy := -2; dy <= 3; dy++ {
		for dx := -2; dx <= 3; dx++ {
			if g.data.Height[(gi.y+dy)*gridSize+gi.x+dx] != locked[i] {
				changed = true
			}
			i++
		}
	}
	if !changed {
		t.Fatal("opening the gate changed no height")
	}
	// The minute timer arms the gate, then locks it again.
	g.gateTimer()
	if gate >= 20 {
		if gi.status != gateOpen {
			t.Fatal("closed on the first tick")
		}
		g.gateTimer()
		if gi.status != gateLocked {
			t.Fatal("not closed on the second tick")
		}
		i = 0
		for dy := -2; dy <= 3; dy++ {
			for dx := -2; dx <= 3; dx++ {
				if g.data.Height[(gi.y+dy)*gridSize+gi.x+dx] != locked[i] {
					t.Fatalf("height at %+d,%+d not restored", dx, dy)
				}
				i++
			}
		}
	}
}

func TestDropPickDecay(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	x, y := int(g.mobs[1].target.X), int(g.mobs[1].target.Y)
	pl.Inventory[2] = wire.Item{Index: 412}
	drop := func() {
		g.requestDropItem(u, packet(&DropGroundItem{Header: wire.NewHeader(opCliDropItem, 1), InvType: wire.SlotInv, InvSlot: 2, PosX: int16(x + 1), PosY: int16(y)}))
	}
	drop()
	if pl.Inventory[2].Index != 412 {
		t.Fatal("a player (not GM) dropped an item")
	}
	u.accessLevel = 1
	drop()
	if pl.Inventory[2].Index != 0 {
		t.Fatal("GM drop failed")
	}
	id := 0
	for i := g.initCount + 1; i < maxGroundItem; i++ {
		if g.ground[i].open && g.ground[i].item.Index == 412 {
			id = i
		}
	}
	if id == 0 {
		t.Fatal("dropped item not on the ground")
	}
	gi := g.ground[id]

	pick := func(id int) {
		g.requestPickItem(u, packet(&PickItem{Header: wire.NewHeader(opCliPickItem, 1), InvType: wire.SlotInv, InitID: int16(id + groundIDBase), PosX: int16(gi.x), PosY: int16(gi.y)}))
	}
	pick(id)
	if g.ground[id].open || g.itemAt(gi.x, gi.y) != 0 {
		t.Fatal("item still on the ground after pick")
	}
	if s := firstSlotOf(pl, 412); s < 0 {
		t.Fatal("picked item not in the inventory")
	}

	// Gates cannot be picked up.
	if g.initCount > 0 {
		gate := g.ground[1]
		g.mobs[1].target = wire.Position{X: uint16(gate.x), Y: uint16(gate.y)}
		g.requestPickItem(u, packet(&PickItem{Header: wire.NewHeader(opCliPickItem, 1), InvType: wire.SlotInv, InitID: groundIDBase + 1, PosX: int16(gate.x), PosY: int16(gate.y)}))
		if !g.ground[1].open {
			t.Fatal("picked up a gate")
		}
		g.mobs[1].target = wire.Position{X: uint16(x), Y: uint16(y)}
	}

	// Dropped items decay after two passes of the cursor.
	pl.Inventory[3] = wire.Item{Index: 413}
	g.requestDropItem(u, packet(&DropGroundItem{Header: wire.NewHeader(opCliDropItem, 1), InvType: wire.SlotInv, InvSlot: 3, PosX: int16(x - 1), PosY: int16(y)}))
	open := func() bool {
		for i := g.initCount + 1; i < maxGroundItem; i++ {
			if g.ground[i].open && g.ground[i].item.Index == 413 {
				return true
			}
		}
		return false
	}
	if !open() {
		t.Fatal("second drop failed")
	}
	for range 2 * maxGroundItem / 8 {
		g.decayGroundItems()
	}
	if open() {
		t.Fatal("dropped item did not decay")
	}
	for id := 1; id <= g.initCount; id++ {
		if !g.ground[id].open {
			t.Fatalf("gate %d decayed", id)
		}
	}
}
