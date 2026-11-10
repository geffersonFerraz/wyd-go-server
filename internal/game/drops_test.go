package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestLoadRealDropTable(t *testing.T) {
	g := worldGame(t)
	d := g.data.Drops["Guer Caveira "]
	if d == nil || d.MaxDrops != 19 || d.Gold != 20000 || len(d.Items) != 19 {
		t.Fatalf("Guer Caveira: %+v", d)
	}
	if it := d.Items[5]; it.Kills != 250 || it.Index != 911 || it.Effect != 2 || it.Value != 45 {
		t.Fatalf("item 5: %+v", it)
	}
	if g.data.Drops["Guer Caveira"] == nil {
		t.Fatal(`"Guer Caveira" (no trailing space) is a different monster and must be kept`)
	}
	// Spawned monster names must match table names exactly.
	matched := map[string]bool{}
	for id := firstNPC; id < MaxMob; id++ {
		if g.mobs[id].mode != 0 {
			if n := mobName(&g.mobs[id].info.Player); g.data.Drops[n] != nil {
				matched[n] = true
			}
		}
	}
	if len(matched) == 0 {
		t.Fatal("no spawned monster name matches the drop table")
	}
	t.Logf("%d drop table entries, %d matched by spawned monsters", len(g.data.Drops), len(matched))
}

func writeDrops(t *testing.T, g *Game, mobs ...*MobDrop) {
	t.Helper()
	g.data.Drops = map[string]*MobDrop{}
	for _, m := range mobs {
		g.data.Drops[m.Name] = m
	}
}

// testOrigin is a monster killed next to player 1.
func testOrigin(g *Game) *dropOrigin {
	t := g.mobs[1].target
	return &dropOrigin{mob: "Teste", x: int(t.X) + 2, y: int(t.Y), killerAccount: "DONO", killerChar: "Dono"}
}

// groundItems counts the ground items of an index and their amounts.
func groundItems(g *Game, index uint16) (n, amount int) {
	for i := g.initCount + 1; i < maxGroundItem; i++ {
		if gi := &g.ground[i]; gi.open && gi.item.Index == index {
			n++
			amount += max(itemAmount(&gi.item), 1)
		}
	}
	return n, amount
}

func TestDropEveryNKills(t *testing.T) {
	g, a, _ := dropScene(t)
	g.cfg.DropToInventory = nil
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Gold: 100, Items: []DropItem{{Kills: 3, Index: 1103}, {Kills: 1, Index: 4117}}})
	pl := &g.mobs[1].info.Player
	pl.Gold = 0
	before := pl.Inventory
	for range 3 {
		g.handleMobDrop(a, "Teste", testOrigin(g))
	}
	if pl.Gold != 300 {
		t.Fatalf("gold %d, want 300", pl.Gold)
	}
	if pl.Inventory != before {
		t.Fatal("a drop went into the inventory instead of the ground")
	}
	if n, _ := groundItems(g, 1103); n != 1 {
		t.Fatalf("%d x 1103 on the ground after 3 kills, want 1", n)
	}
	if _, amount := groundItems(g, 4117); amount != 3 {
		t.Fatalf("4117 on the ground: %d, want 3", amount)
	}
	id := groundIDOf(g, 1103)
	if gi := &g.ground[id]; gi.owner != "Dono" || gi.origin == nil || gi.origin.mob != "Teste" ||
		gi.expireAt-g.now() != dropLifetimeMs || gi.ownerUntil-g.now() != dropOwnerMs {
		t.Fatalf("ground drop: %+v", gi)
	}
}

// The items of drop_to_inventory skip the ground: they stack in the
// inventory, and a non-stackable one is tracked with the monster as origin.
func TestDropToInventory(t *testing.T) {
	g, a, _ := dropScene(t)
	g.cfg.DropToInventory = []uint16{4117, 1103}
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Items: []DropItem{{Kills: 1, Index: 4117}, {Kills: 1, Index: 1103}, {Kills: 1, Index: 1104}}})
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	for range 2 {
		g.handleMobDrop(a, "Teste", testOrigin(g))
	}
	if n, _ := groundItems(g, 4117); n != 0 {
		t.Fatal("4117 fell on the ground")
	}
	if n, _ := groundItems(g, 1103); n != 0 {
		t.Fatal("1103 fell on the ground")
	}
	if n, _ := groundItems(g, 1104); n != 2 {
		t.Fatalf("%d x 1104 on the ground, want 2 (not in the list)", n)
	}
	if s := firstSlotOf(pl, 4117); s < 0 || itemAmount(&pl.Inventory[s]) != 2 {
		t.Fatalf("4117 not stacked in the inventory: slot %d", s)
	}
	swords := 0
	for i := range invSlots {
		if pl.Inventory[i].Index == 1103 {
			swords++
		}
	}
	if swords != 2 {
		t.Fatalf("%d x 1103 in the inventory, want 2", swords)
	}
	if len(a.tracks) != 2 || a.tracks[0].item.Index != 1103 || a.tracks[1].item.Index != 1103 {
		t.Fatalf("tracks: %d", len(a.tracks))
	}
}

func TestDropFilter(t *testing.T) {
	g, a, _ := dropScene(t)
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Items: []DropItem{{Kills: 1, Index: 1103}, {Kills: 1, Index: 1104}}})
	ci := &g.mobs[1].info
	ci.FilterDrop.IsEnabled = true
	ci.FilterDrop.Items[0] = wire.Item{Index: 1104}
	g.handleMobDrop(a, "Teste", testOrigin(g))
	if n, _ := groundItems(g, 1103); n != 0 {
		t.Fatal("filtered item dropped")
	}
	if n, _ := groundItems(g, 1104); n != 1 {
		t.Fatal("allowed item not dropped")
	}
}

func dropStack(id uint16, n int) wire.Item {
	return wire.Item{Index: id, Effect: [3]wire.ItemEffect{{Index: EF_AMOUNT, Value: uint16(n)}}}
}

func TestDropStacksWithoutFairy(t *testing.T) {
	g, a, _ := tradeGame(t)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	const potion = 412 // stackable, outside 4117..4121

	first := wire.Item{Index: potion}
	g.giveDrop(a, &first, false)
	second := wire.Item{Index: potion}
	g.giveDrop(a, &second, false)
	if got := itemAmount(&pl.Inventory[0]); pl.Inventory[0].Index != potion || got != 2 || pl.Inventory[1].Index != 0 {
		t.Fatalf("not stacked: %+v / %+v", pl.Inventory[0], pl.Inventory[1])
	}
}

func TestDropOverflowsFullStacks(t *testing.T) {
	g, a, _ := tradeGame(t)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	const potion = 412
	pl.Inventory[0] = dropStack(potion, maxStackAmount)
	pl.Inventory[1] = dropStack(potion, maxStackAmount-1)

	drop := dropStack(potion, 3)
	g.giveDrop(a, &drop, false)
	if itemAmount(&pl.Inventory[0]) != maxStackAmount || itemAmount(&pl.Inventory[1]) != maxStackAmount || itemAmount(&pl.Inventory[2]) != 2 {
		t.Fatalf("overflow: %d %d %d", itemAmount(&pl.Inventory[0]), itemAmount(&pl.Inventory[1]), itemAmount(&pl.Inventory[2]))
	}
}

func TestDropNotStackableTakesNewSlot(t *testing.T) {
	g, a, _ := tradeGame(t)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	const sword = 1103
	first, second := wire.Item{Index: sword}, wire.Item{Index: sword}
	g.giveDrop(a, &first, true)
	g.giveDrop(a, &second, true)
	// Item 2x2: the second one starts right after the cells of the first.
	if pl.Inventory[0].Index != sword || pl.Inventory[1].Index != 0 || pl.Inventory[2].Index != sword {
		t.Fatalf("got %+v / %+v / %+v", pl.Inventory[0], pl.Inventory[1], pl.Inventory[2])
	}
}

func TestDropKeptWhenStackHasNoEffectSlot(t *testing.T) {
	g, a, _ := tradeGame(t)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	const potion = 412
	pl.Inventory[0] = wire.Item{Index: potion, Effect: [3]wire.ItemEffect{{Index: 1, Value: 1}, {Index: 2, Value: 1}, {Index: 3, Value: 1}}}
	drop := wire.Item{Index: potion}
	g.giveDrop(a, &drop, false)
	if pl.Inventory[1].Index != potion {
		t.Fatalf("drop lost: %+v", pl.Inventory[1])
	}
}
