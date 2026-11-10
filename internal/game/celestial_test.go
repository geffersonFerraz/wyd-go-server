package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func celestialAt(g *Game, level int16, ev uint8) (*User, *Mob) {
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	m := &g.mobs[1]
	pl := &m.info.Player
	pl.ClassInfo = 0
	pl.Equip[0].Effect[1].Value = uint16(ev)
	pl.BStatus.Level, pl.Status.Level = level, level
	pl.BStatus.STR, pl.BStatus.INT = int64(baseSIDCHM[0][0]), int64(baseSIDCHM[0][1])
	pl.BStatus.DEX, pl.BStatus.CON = int64(baseSIDCHM[0][2]), int64(baseSIDCHM[0][3])
	return u, m
}

func TestCelestialPoints(t *testing.T) {
	g := worldGame(t)
	_, m := celestialAt(g, 10, evoCelestial)
	m.info.Info = questLevel355 | questElime
	g.getCurrentScore(1)
	if got, want := m.info.Player.StatusPoint, uint16(100+100+1001+10*6); got != want {
		t.Fatalf("Celestial status points %d, want %d", got, want)
	}
	if got, want := m.info.Player.SkillPoint, uint16(600+10*4); got != want {
		t.Fatalf("Celestial skill points %d, want %d", got, want)
	}
	// With a Sub-Celestial: its level and the resets count too.
	m.info.Sub.Status = 1
	m.info.Sub.SubStatus.Level = 160
	m.info.Info |= questReset1 | questReset2
	g.getCurrentScore(1)
	want := 100 + 100 + 2*200 + 1001 + 10*6 + 160*9 + (160-149)*4
	if got := int(m.info.Player.StatusPoint); got != want {
		t.Fatalf("with sub: %d, want %d", got, want)
	}
}

func TestCelestialLevelBlocks(t *testing.T) {
	g := worldGame(t)
	u, m := celestialAt(g, 198, evoCelestial)
	pl := &m.info.Player
	m.info.Info = questUnlock39 | questUnlock89 | questUnlock200 // past the early blocks
	m.info.Info &^= questUnlock200
	// 199 without the three resets: the level comes, the extra exp goes.
	pl.Exp = nextLevel[evoCelestial][198] + 12345
	g.checkGetLevel(u)
	if pl.BStatus.Level != 199 || pl.Exp != nextLevel[evoCelestial][198] || m.info.Info&questLvBlocked != 0 {
		t.Fatalf("no resets: level %d exp %d info %x", pl.BStatus.Level, pl.Exp, m.info.Info)
	}
	// With the three resets the level is blocked until unlocked.
	pl.BStatus.Level, pl.Status.Level = 198, 198
	m.info.Info |= questReset1 | questReset2 | questReset3
	pl.Exp = nextLevel[evoCelestial][198] + 12345
	g.checkGetLevel(u)
	if m.info.Info&questLvBlocked == 0 || pl.Exp != nextLevel[evoCelestial][198] {
		t.Fatalf("3 resets: info %x exp %d", m.info.Info, pl.Exp)
	}
}

func TestSubCelestialReset(t *testing.T) {
	g := worldGame(t)
	u, m := celestialAt(g, 199, evoSubCele)
	pl := &m.info.Player
	m.info.Info = questArcana
	m.info.Fame = 1500
	pl.Exp = 4_100_000_000
	pl.Inventory[0] = wire.Item{Index: 3020}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 2}
	g.useFuryStone(u, &pl.Inventory[0])
	if m.info.Info&questReset1 == 0 || m.info.Sub.Info&questReset1 == 0 {
		t.Fatalf("reset not recorded: %x / %x", m.info.Info, m.info.Sub.Info)
	}
	if m.info.Fame != 500 || itemAmount(&pl.Inventory[0]) != 1 {
		t.Fatalf("fame %d, stones %d", m.info.Fame, itemAmount(&pl.Inventory[0]))
	}
	if pl.Exp != 4_100_000_000-40_000_000 {
		t.Fatalf("exp %d", pl.Exp)
	}
	m.info.Fame = 0
	g.useFuryStone(u, &pl.Inventory[0])
	if m.info.Info&questReset2 != 0 {
		t.Fatal("reset without fame")
	}
}

func TestPedraIdeal(t *testing.T) {
	g := worldGame(t)
	u, m := celestialAt(g, 399, evoArch)
	listen(u)
	pl := &m.info.Player
	use := func() {
		pl.Inventory[0] = wire.Item{Index: 1}
		g.usePedraIdeal(u, &wire.UseItem{SrcType: wire.SlotInv, SrcSlot: 0}, &pl.Inventory[0])
	}
	pl.CapeInfo = capeBlue
	pl.Equip[0].Effect[1].Index = 6 // an Arch TransKnight
	m.info.MortalSlot = 1
	u.charList.Status[1].Level = 300 // the mortal is not 400 yet
	use()
	if evolution(pl) != evoArch {
		t.Fatal("Celestial without a level 400 mortal")
	}
	u.charList.Status[1].Level = 399
	use()
	if evolution(pl) != evoCelestial || pl.Equip[1].Index != 3502 || pl.Equip[15].Index != 3197 || m.info.Info&questLevel399 == 0 {
		t.Fatalf("Celestial: ev %d cythera %d cape %d info %x", evolution(pl), pl.Equip[1].Index, pl.Equip[15].Index, m.info.Info)
	}
	if pl.BStatus.STR != 8 || pl.Learn[0] != 1<<30 {
		t.Fatalf("Celestial base: STR %d learn %x", pl.BStatus.STR, pl.Learn[0])
	}

	// Celestial 120 with a sephirot -> Sub-Celestial.
	u.status = userPlay
	g.mobs[1].mode = 1
	pl.BStatus.Level, pl.Status.Level = 150, 150
	pl.Equip[11] = wire.Item{Index: 1762}
	use()
	if m.info.Sub.Status != 1 || pl.Equip[11].Index != 0 || m.info.Sub.Equip[0].Index != 6+2 {
		t.Fatalf("Sub-Celestial: %+v equip11 %d", m.info.Sub.Equip[0], pl.Equip[11].Index)
	}

	// Level 200 unlock.
	u.status = userPlay
	g.mobs[1].mode = 1
	pl.BStatus.Level = 199
	m.info.Sub.SubStatus.Level = 199
	m.info.Info |= questReset1 | questReset2 | questReset3 | questLvBlocked
	m.info.Fame = 1000
	pl.Inventory[5] = wire.Item{Index: itemSeloReal}
	use()
	if m.info.Info&questUnlock200 == 0 || m.info.Info&questLvBlocked != 0 || pl.Inventory[5].Index != 0 || m.info.Fame != 0 {
		t.Fatalf("unlock: info %x seal %d fame %d", m.info.Info, pl.Inventory[5].Index, m.info.Fame)
	}
}

func TestMountHunger(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	tick := func() {
		u.regenTurns = 449
		g.mountTimer(u)
	}
	pl.Equip[14] = wire.Item{Index: 2361}
	setMountHP(&pl.Equip[14], 100)
	pl.Equip[14].Effect[2].Index = 50
	tick()
	if pl.Equip[14].Effect[2].Index != 48 || mountHP(&pl.Equip[14]) != 100 {
		t.Fatalf("hungry adult: %+v", pl.Equip[14])
	}
	pl.Equip[14].Effect[2].Index = 3
	tick()
	// (ProcessAdultMount then clears the feed of a dead adult, as in the C++.)
	if mountHP(&pl.Equip[14]) != 0 {
		t.Fatalf("starved adult: %+v", pl.Equip[14])
	}
	pl.Equip[14] = wire.Item{Index: 2331}
	setMountHP(&pl.Equip[14], 100)
	pl.Equip[14].Effect[2].Index = 2
	tick()
	if mountHP(&pl.Equip[14]) != 0 || pl.Equip[14].Effect[2].Index != 4 {
		t.Fatalf("starved baby: %+v", pl.Equip[14])
	}
	// Eggs incubate.
	pl.Equip[14] = wire.Item{Index: 2301}
	pl.Equip[14].Effect[2] = wire.ItemEffect{Index: EF_INCUDELAY, Value: 5}
	tick()
	if pl.Equip[14].Effect[2].Value != 4 {
		t.Fatalf("egg: %+v", pl.Equip[14])
	}
}
