package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func addPoint(g *Game, u *User, mode, info int16) {
	g.requestAddPoint(u, packet(&AddPoint{Header: wire.NewHeader(opCliAddPoint, uint16(u.id)), Mode: mode, Info: info}))
}

func TestAddStatusAndMastery(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	// Points are derived from the level by GetCurrentScore (what is left
	// after the base stats), so spending one shows as one less.
	for _, level := range []int16{10, 120} {
		pl.BStatus.Level, pl.Status.Level = level, level
		pl.BStatus.STR, pl.BStatus.INT, pl.BStatus.DEX, pl.BStatus.CON = 0, 0, 0, 0
		g.getCurrentScore(1)
		pts := pl.StatusPoint
		want := uint16(1)
		if pts > 200 {
			want = 100
		}
		dex := pl.BStatus.DEX
		addPoint(g, u, 0, 2)
		if pl.BStatus.DEX != dex+want || pl.StatusPoint != pts-want {
			t.Fatalf("level %d: dex %d -> %d, points %d -> %d", level, dex, pl.BStatus.DEX, pts, pl.StatusPoint)
		}
		addPoint(g, u, 0, 4) // out of range
		if pl.StatusPoint != pts-want {
			t.Fatal("bad stat index spent a point")
		}
	}
	pl.BStatus.Level, pl.Status.Level = 10, 10

	// Mastery is capped at (level+1)*3/2 for non-Celestials (Mortal 10:
	// 20 points).
	g.getCurrentScore(1)
	for range 30 {
		addPoint(g, u, 1, 1)
	}
	if want := uint16((10 + 1) * 3 >> 1); pl.BStatus.Mastery[1] != want {
		t.Fatalf("mastery %d, want cap %d", pl.BStatus.Mastery[1], want)
	}
	if pl.MasterPoint != 20-pl.BStatus.Mastery[1] {
		t.Fatalf("master points %d", pl.MasterPoint)
	}
	// Celestials stop at 200 without the 8th skill.
	pl.Equip[0].Effect[1].Value = evoCelestial
	pl.BStatus.Mastery[2] = 200
	addPoint(g, u, 1, 2)
	if pl.BStatus.Mastery[2] != 200 {
		t.Fatal("mastery went past 200")
	}
	pl.Learn[0] |= 1 << 15 // the 8th skill of tree 2
	addPoint(g, u, 1, 2)
	if pl.BStatus.Mastery[2] != 201 {
		t.Fatal("8th skill did not lift the cap")
	}
}

func TestLearnSkill(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 50_000_000)
	pl := &g.mobs[1].info.Player
	pl.ClassInfo = 1
	pl.Status.Level, pl.BStatus.Level = 399, 399
	pl.BStatus.Mastery = [4]uint16{200, 200, 200, 200} // 5031 needs 200 in the second tree
	g.getCurrentScore(1)

	book := int16(5000 + 24*1 + 3)
	addPoint(g, u, 2, book)
	if pl.Learn[0]&(1<<3) == 0 {
		t.Fatalf("skill not learned (points left %d)", pl.SkillPoint)
	}
	before := pl.SkillPoint
	addPoint(g, u, 2, book)
	if pl.SkillPoint != before {
		t.Fatal("learned twice")
	}
	// Other class's book.
	addPoint(g, u, 2, 5000+5)
	if pl.Learn[0]&(1<<5) != 0 {
		t.Fatal("learned another class's skill")
	}
	// The 8th skill needs the whole tree.
	addPoint(g, u, 2, 5000+24+7)
	if pl.Learn[0]&(1<<7) != 0 {
		t.Fatal("8th skill without its tree")
	}
	pl.Learn[0] |= 0x7F
	gold := pl.Gold
	addPoint(g, u, 2, 5000+24+7)
	if pl.Learn[0]&(1<<7) == 0 || pl.Gold != gold-eighthSkillGold[0] {
		t.Fatalf("8th skill: learn %x gold %d", pl.Learn[0], pl.Gold)
	}
	// Only one 8th skill.
	pl.Learn[0] |= 0x7F00
	addPoint(g, u, 2, 5000+24+15)
	if pl.Learn[0]&(1<<15) != 0 {
		t.Fatal("second 8th skill")
	}
}

func TestSkillBarDropsUnlearned(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	m := &g.mobs[1]
	m.info.Player.Learn[0] = 1<<2 | 1<<24
	p := wire.SkillBar{Header: wire.NewHeader(opCliSkillBar, 1)}
	p.SkillBar1 = [4]uint8{2, 3, 96, 255}
	for i := range p.SkillBar2 {
		p.SkillBar2[i] = 26 // 26 % 24 = bit 2: learned
	}
	g.requestChangeSkillBar(u, packet(&p))
	if got := m.info.Player.SkillBar1; got != [4]uint8{2, skillBarEmpty, 96, skillBarEmpty} {
		t.Fatalf("bar1 %v", got)
	}
	if m.info.SkillBar[0] != 26 {
		t.Fatalf("bar2 %v", m.info.SkillBar)
	}
}

func TestDeleteItem(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[4] = wire.Item{Index: 412}
	pl.Inventory[5] = wire.Item{Index: 509}
	del := func(slot, id int16) {
		g.requestDeleteItem(u, packet(&DeleteItem{Header: wire.NewHeader(opCliDeleteItem, 1), SlotID: slot, ItemID: id}))
	}
	del(4, 413) // wrong id
	if pl.Inventory[4].Index != 412 {
		t.Fatal("deleted with the wrong id")
	}
	del(4, 412)
	if pl.Inventory[4].Index != 0 {
		t.Fatal("not deleted")
	}
	del(5, 509)
	if pl.Inventory[5].Index != 509 {
		t.Fatal("509 deleted")
	}
}

func TestUngroup(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	split := func(slot, id, n int32) {
		u.lastMoveItem = time.Time{}
		g.requestUngroupItem(u, packet(&UngroupItem{Header: wire.NewHeader(opCliUngroup, 1), SlotID: slot, ItemID: id, Amount: n}))
	}
	pl.Inventory[0] = wire.Item{Index: 412}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 10}
	split(0, 412, 3)
	if itemAmount(&pl.Inventory[0]) != 7 || pl.Inventory[1].Index != 412 || itemAmount(&pl.Inventory[1]) != 3 {
		t.Fatalf("split: %+v / %+v", pl.Inventory[0], pl.Inventory[1])
	}
	// The whole stack, or a single item, cannot be split (the C++ copied it).
	split(0, 412, 7)
	pl.Inventory[2] = wire.Item{Index: 413}
	split(2, 413, 1)
	if pl.Inventory[3].Index != 0 || itemAmount(&pl.Inventory[0]) != 7 {
		t.Fatalf("split too much: %+v %+v", pl.Inventory[0], pl.Inventory[3])
	}
	// Not in the list.
	pl.Inventory[0].Index = 401
	split(0, 401, 1)
	if pl.Inventory[3].Index != 0 {
		t.Fatal("split an item that is not in the list")
	}
}

func TestChangeCity(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Info = 1 // merchant bit kept
	g.requestChangeCity(u, packet(&wire.SignalValue{Header: wire.NewHeader(opCliChangeCity, 1), Value: 2}))
	if pl.Info != 1|2<<6 {
		t.Fatalf("info %08b", pl.Info)
	}
}
