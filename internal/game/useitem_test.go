package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// findItem returns an ItemList id whose EF_VOLATILE is vol and that passes ok.
func findItem(t *testing.T, g *Game, vol int, ok func(id uint16) bool) uint16 {
	t.Helper()
	for id := uint16(1); int(id) < MaxItemList; id++ {
		it := wire.Item{Index: id}
		if g.data.itemAbility(&it, EF_VOLATILE) == vol && (ok == nil || ok(id)) {
			return id
		}
	}
	t.Skipf("no item with volatile %d", vol)
	return 0
}

func useSlot(g *Game, u *User, slot int) {
	p := wire.UseItem{Header: wire.NewHeader(wire.OpCliUseItem, uint16(u.id)), SrcType: wire.SlotInv, SrcSlot: uint32(slot)}
	g.requestUseItem(u, packet(&p))
}

func TestPotion(t *testing.T) {
	g := worldGame(t)
	pot := findItem(t, g, 1, func(id uint16) bool { return g.data.effectValueByIndex(id, EF_HP) > 0 })
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Status.Level = 10
	pl.Status.MaxHP, pl.Status.CurHP = 5000, 100
	u.potionHP = 100
	pl.Inventory[3] = wire.Item{Index: pot}
	pl.Inventory[3].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 2}

	useSlot(g, u, 3)
	if !u.quaff || u.potionHP <= 100 {
		t.Fatalf("potion %d: pending hp %d quaff %v", pot, u.potionHP, u.quaff)
	}
	if pl.Inventory[3].Index != pot || itemAmount(&pl.Inventory[3]) != 1 {
		t.Fatalf("stack after use: %+v", pl.Inventory[3])
	}
	g.potionTimer()
	if want := min(int64(100+110), u.potionHP); pl.Status.CurHP != want {
		t.Fatalf("hp %d after one tick, want %d", pl.Status.CurHP, want)
	}
	if u.quaff {
		t.Fatal("quaff not cleared")
	}

	// Affect 32 blocks potions.
	g.mobs[1].info.Affects[0] = wire.Affect{Index: 32, Time: 10}
	before := u.potionHP
	useSlot(g, u, 3)
	if u.potionHP != before || pl.Inventory[3].Index != pot {
		t.Fatal("potion used under affect 32")
	}
}

func TestReturnScroll(t *testing.T) {
	g := worldGame(t)
	scroll := findItem(t, g, 11, nil)
	// Somewhere outside a village.
	x, y := 0, 0
	for _, c := range g.data.Teleports {
		if g.data.attribute(c.DestX, c.DestY)&attrVillage == 0 && !g.data.Blocked(c.DestX, c.DestY) {
			x, y = c.DestX, c.DestY
			break
		}
	}
	if x == 0 {
		t.Skip("no field location")
	}
	u := placePlayer(g, x, y, 0)
	pl := &g.mobs[1].info.Player
	start := g.mobs[1].target
	pl.Inventory[0] = wire.Item{Index: scroll}
	useSlot(g, u, 0)
	if g.mobs[1].target == start || u.returnX == 0 {
		t.Fatalf("not recalled: at %v, return point %d,%d", g.mobs[1].target, u.returnX, u.returnY)
	}
	if pl.Inventory[0].Index != 0 {
		t.Fatal("scroll not consumed")
	}
	// In town the scroll takes the player back.
	if g.data.attribute(int(g.mobs[1].target.X), int(g.mobs[1].target.Y))&attrVillage != 0 {
		pl.Inventory[0] = wire.Item{Index: scroll}
		useSlot(g, u, 0)
		if p := g.mobs[1].target; distance(int(p.X), int(p.Y), int(start.X), int(start.Y)) > 6 {
			t.Fatalf("not taken back: at %v, want near %v", p, start)
		}
	}
}

func TestPillAndExpItems(t *testing.T) {
	g := worldGame(t)
	pill := findItem(t, g, 6, nil)
	eye := findItem(t, g, 8, nil)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: pill}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 2}
	useSlot(g, u, 0)
	if g.mobs[1].info.Info&questPilula == 0 || itemAmount(&pl.Inventory[0]) != 1 {
		t.Fatal("pill not taken")
	}
	useSlot(g, u, 0) // a second pill is wasted, like the C++
	if pl.Inventory[0].Index != 0 {
		t.Fatal("second pill kept")
	}
	exp := pl.Exp
	pl.Inventory[1] = wire.Item{Index: eye}
	useSlot(g, u, 1)
	if pl.Exp != exp+3000 || pl.Inventory[1].Index != 0 {
		t.Fatalf("crescent eye: exp %d", pl.Exp-exp)
	}
}

func TestExtremeStone(t *testing.T) {
	g := worldGame(t)
	var dstID, stone uint16
	for id := uint16(1); int(id) < MaxItemList && dstID == 0; id++ {
		d := g.data.Item(id)
		it := wire.Item{Index: id}
		if s := extremeUnique(int(d.Unique)); s >= 0 && d.Pos >= 2 && d.Pos <= 32 && d.Extreme > 0 &&
			g.data.itemAbility(&it, EF_UNKNOW1) != 5 {
			dstID, stone = id, uint16(575+s)
		}
	}
	if dstID == 0 || g.data.itemAbility(&wire.Item{Index: stone}, EF_VOLATILE) != 9 {
		t.Skip("no extreme item / stone in the data")
	}
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: stone}
	pl.Inventory[4] = wire.Item{Index: dstID}
	fixRolls(t, 99)
	useOn(g, u, 0, wire.SlotInv, 4)
	if pl.Inventory[4].Index != uint16(g.data.Item(dstID).Extreme) || pl.Inventory[0].Index != 0 {
		t.Fatalf("item %d -> %d, want %d", dstID, pl.Inventory[4].Index, g.data.Item(dstID).Extreme)
	}
}

func TestFeedMount(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Equip[14] = wire.Item{Index: 2360 + 2} // adult mount, family 2
	setMountHP(&pl.Equip[14], 100)
	food := uint16(2420 + 2)
	if g.data.itemAbility(&wire.Item{Index: food}, EF_VOLATILE) != 15 {
		t.Skip("food 2422 is not volatile 15 here")
	}
	pl.Inventory[0] = wire.Item{Index: food}
	useOn(g, u, 0, wire.SlotEquip, 14)
	maxHP := int(g.data.NPCBase[12].BStatus.MaxHP)
	if maxHP <= 0 {
		t.Fatal("npc_base not loaded")
	}
	if want := min(5100, maxHP); mountHP(&pl.Equip[14]) != want || pl.Equip[14].Effect[2].Index != 2 {
		t.Fatalf("mount hp %d (want %d), feed %d", mountHP(&pl.Equip[14]), want, pl.Equip[14].Effect[2].Index)
	}
	// Wrong food is refused.
	pl.Inventory[0] = wire.Item{Index: food + 1}
	useOn(g, u, 0, wire.SlotEquip, 14)
	if pl.Inventory[0].Index != food+1 {
		t.Fatal("wrong food eaten")
	}
}

func TestUnportedItemIsKept(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	// An equipment piece has no use effect.
	pl.Inventory[2] = wire.Item{Index: 1103}
	useSlot(g, u, 2)
	if pl.Inventory[2].Index != 1103 {
		t.Fatal("item without a use was consumed")
	}
}
