package game

import (
	"testing"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func rateGame(t *testing.T, rate ...RateItem) (*Game, *Mob) {
	g := warGame(t, 1)
	for i := range g.data.DropSlots {
		g.data.DropSlots[i] = store.DropSlot{Slot: i, Base: 400, Rate: 100}
	}
	g.data.BlockedDrops = map[int]bool{413: true}
	g.data.Drops = map[string]*MobDrop{"Teste": {Name: "Teste", Rate: rate}}
	dead := &Mob{}
	wire.PutCString(dead.info.Player.Name[:], "Teste")
	return g, dead
}

func TestRateDropChanceOfOne(t *testing.T) {
	g, dead := rateGame(t, RateItem{Slot: 5, Item: wire.Item{Index: 412}, Chance: 1})
	g.rateDrop(g.users[1], dead)
	if firstSlotOf(&g.mobs[1].info.Player, 412) < 0 {
		t.Fatal("chance 1 did not drop")
	}
}

func TestRateDropBlocked(t *testing.T) {
	g, dead := rateGame(t, RateItem{Slot: 5, Item: wire.Item{Index: 413}, Chance: 1})
	g.rateDrop(g.users[1], dead)
	if firstSlotOf(&g.mobs[1].info.Player, 413) >= 0 {
		t.Fatal("a blocked item dropped")
	}
}

func TestRateDropSlot11Guaranteed(t *testing.T) {
	g, dead := rateGame(t, RateItem{Slot: 11, Item: wire.Item{Index: 412}})
	g.rateDrop(g.users[1], dead)
	if firstSlotOf(&g.mobs[1].info.Player, 412) < 0 {
		t.Fatal("slot 11 did not drop")
	}
}

func TestRateDropSlotOff(t *testing.T) {
	g, dead := rateGame(t, RateItem{Slot: 5, Item: wire.Item{Index: 412}})
	g.data.DropSlots[5].Base = 0
	for range 200 {
		g.rateDrop(g.users[1], dead)
	}
	if firstSlotOf(&g.mobs[1].info.Player, 412) >= 0 {
		t.Fatal("a slot with base 0 dropped")
	}
}

func TestRateDropNameUsesSpaces(t *testing.T) {
	g, dead := rateGame(t)
	g.data.Drops = map[string]*MobDrop{"Lobo Negro": {Name: "Lobo Negro", Rate: []RateItem{{Slot: 1, Item: wire.Item{Index: 412}, Chance: 1}}}}
	wire.PutCString(dead.info.Player.Name[:], "Lobo_Negro")
	g.rateDrop(g.users[1], dead)
	if firstSlotOf(&g.mobs[1].info.Player, 412) < 0 {
		t.Fatal("name with underscore not matched")
	}
}

func TestChanceDivisorUsesSlotRate(t *testing.T) {
	g, _ := rateGame(t)
	g.data.DropSlots[20] = store.DropSlot{Slot: 20, Base: 20000, Rate: 500}
	r := &RateItem{Slot: 20, Item: wire.Item{Index: 412}}
	if d := g.chanceDivisor(g.users[1], 300, r); d != 4000 {
		t.Fatalf("divisor %d, want 4000", d)
	}
	r.Chance = 7
	if d := g.chanceDivisor(g.users[1], 300, r); d != 7 {
		t.Fatalf("own chance: %d", d)
	}
}

// The first import reads the real generators and leaves shops alone.
func TestReadRateDropsFromFiles(t *testing.T) {
	list, err := ReadRateDrops(ConfigDir, WorldFixture(t))
	if err != nil || len(list) < 100 {
		t.Fatalf("%d monsters, err %v", len(list), err)
	}
	for _, d := range list {
		for _, c := range d.Name {
			if c == '_' {
				t.Fatalf("name kept the underscore: %q", d.Name)
			}
		}
	}
}

// The area attack is one 0x367 for every target.
func TestAreaAttackIsOnePacket(t *testing.T) {
	g := warGame(t, 2)
	x, y := 2100, 2100
	for i, id := range []int{1, 2} {
		g.clearGrid(id)
		g.mobs[id].target = wire.Position{X: uint16(x + i), Y: uint16(y)}
		g.setCell(x+i, y, uint16(id))
		g.users[id].out = nil
		g.mobs[id].info.Player.Status.CurHP, g.mobs[id].info.Player.Status.MaxHP = 1000, 1000
	}
	boss := firstNPC + 7
	m := &g.mobs[boss]
	m.mode = modeBattle
	m.target = wire.Position{X: uint16(x - 2), Y: uint16(y)}
	m.info.Player.Status.DEX = 20
	m.info.Player.Status.Attack = 50
	seen := 0
	for range 30 {
		g.users[1].out, g.users[2].out = nil, nil
		g.areaAttack(boss)
		seen += len(sent(g.users[1], wire.OpCliAttack))
	}
	if seen == 0 {
		t.Fatal("no 0x367 reached the player")
	}
}
