package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// fixRolls makes rnd() return the given values in turn (then the last one).
func fixRolls(t *testing.T, rolls ...int) {
	t.Helper()
	old := rnd
	i := 0
	rnd = func() int {
		v := rolls[min(i, len(rolls)-1)]
		i++
		return v
	}
	t.Cleanup(func() { rnd = old })
}

func useOn(g *Game, u *User, srcSlot, dstType, dstSlot int) {
	p := wire.UseItem{Header: wire.NewHeader(wire.OpCliUseItem, uint16(u.id)), SrcType: wire.SlotInv, SrcSlot: uint32(srcSlot),
		DstType: uint32(dstType), DstSlot: uint32(dstSlot)}
	g.requestUseItem(u, packet(&p))
}

// refineTarget finds a plain refinable piece of equipment.
func refineTarget(t *testing.T, g *Game) uint16 {
	t.Helper()
	for id := uint16(1000); int(id) < MaxItemList; id++ {
		d := g.data.Item(id)
		if d.Pos > 0 && d.Pos <= 32 && g.data.effectValueByIndex(id, EF_MOBTYPE) == 0 &&
			g.data.effectValueByIndex(id, 125) == 0 && g.data.effectValueByIndex(id, EF_VOLATILE) == 0 {
			return id
		}
	}
	t.Skip("no refinable item")
	return 0
}

func TestSetItemSanc(t *testing.T) {
	var it wire.Item
	if !setItemSanc(&it, 3, 1) || it.Effect[0] != (wire.ItemEffect{Index: EF_SANC, Value: 13}) || itemSanc(&it) != 3 {
		t.Fatalf("+3: %+v", it.Effect)
	}
	setItemSanc(&it, 11, 0)
	if it.Effect[0].Value != 234 || itemSanc(&it) != 11 {
		t.Fatalf("+11: %+v sanc %d", it.Effect[0], itemSanc(&it))
	}
	it.Effect[0].Value = 235 // +11 with a gem bonus
	setItemSanc(&it, 12, 0)
	if it.Effect[0].Value != 239 {
		t.Fatalf("+12 keeps the gem: %d", it.Effect[0].Value)
	}
	full := wire.Item{Effect: [3]wire.ItemEffect{{Index: 1}, {Index: 2}, {Index: 3}}}
	if setItemSanc(&full, 1, 0) {
		t.Fatal("refined an item with no free effect")
	}
}

func TestRefineWithDust(t *testing.T) {
	g := worldGame(t)
	ori := findItem(t, g, 4, nil)
	lac := findItem(t, g, 5, nil)
	target := refineTarget(t, g)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player

	// +0 -> +1 with Oriharucon is always 100%.
	fixRolls(t, 100)
	pl.Inventory[0] = wire.Item{Index: ori}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 3}
	pl.Inventory[5] = wire.Item{Index: target}
	useOn(g, u, 0, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 1 || itemAmount(&pl.Inventory[0]) != 2 {
		t.Fatalf("after Ori: sanc %d, dust %d", itemSanc(&pl.Inventory[5]), itemAmount(&pl.Inventory[0]))
	}

	// A failed roll keeps the level and uses the dust.
	setItemSanc(&pl.Inventory[5], 5, 0)
	fixRolls(t, 100)
	useOn(g, u, 0, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 5 || itemAmount(&pl.Inventory[0]) != 1 {
		t.Fatalf("failed +6: sanc %d, dust %d", itemSanc(&pl.Inventory[5]), itemAmount(&pl.Inventory[0]))
	}

	// Oriharucon stops at +6; the dust is kept.
	setItemSanc(&pl.Inventory[5], 6, 0)
	useOn(g, u, 0, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 6 || pl.Inventory[0].Index != ori {
		t.Fatal("Ori refined past +6")
	}

	// Lactolerium goes on to +9.
	pl.Inventory[1] = wire.Item{Index: lac}
	fixRolls(t, 0)
	useOn(g, u, 1, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 7 || pl.Inventory[1].Index != 0 {
		t.Fatalf("Lac +7: sanc %d", itemSanc(&pl.Inventory[5]))
	}
	setItemSanc(&pl.Inventory[5], 9, 0)
	pl.Inventory[1] = wire.Item{Index: lac}
	useOn(g, u, 1, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 9 || pl.Inventory[1].Index != lac {
		t.Fatal("Lac refined an unequipped item past +9")
	}

	// The face cannot be refined.
	face := pl.Equip[0]
	useOn(g, u, 1, wire.SlotEquip, 0)
	if pl.Equip[0] != face || pl.Inventory[1].Index != lac {
		t.Fatal("refined the face")
	}
}

func TestRefineLacCore(t *testing.T) {
	g := worldGame(t)
	lac := findItem(t, g, 5, nil)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: lac}
	pl.Inventory[1] = wire.Item{Index: nucleoLac}
	useOn(g, u, 0, wire.SlotInv, 1)
	if pl.Inventory[1].Effect[0] != (wire.ItemEffect{Index: EF_AMOUNT, Value: 2}) || pl.Inventory[0].Index != 0 {
		t.Fatalf("core: %+v", pl.Inventory[1].Effect[0])
	}
}

func TestMotionRelayed(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	g.requestMotion(u, packet(&wire.Motion{Header: wire.NewHeader(wire.OpCliMotion, 1), Motion: 15}))
	if g.mobs[1].motion != 1 {
		t.Fatalf("motion %d", g.mobs[1].motion)
	}
}
