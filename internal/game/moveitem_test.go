package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func moveGame(t *testing.T) (*Game, *User, *wire.Mob) {
	g := warGame(t, 1)
	z := &g.data.Cities[0]
	g.mobs[1].target = wire.Position{X: uint16(z.CityMinX + 1), Y: uint16(z.CityMinY + 1)}
	return g, g.users[1], &g.mobs[1].info.Player
}

func moveItem(g *Game, u *User, srcType, srcSlot, dstType, dstSlot int) {
	p := wire.MoveItem{Header: wire.NewHeader(wire.OpCliMoveItem, uint16(u.id)), SrcType: uint8(srcType), SrcSlot: uint8(srcSlot), DstType: uint8(dstType), DstSlot: uint8(dstSlot)}
	g.requestMoveItem(u, packet(&p))
}

func TestMoveStacksSameItem(t *testing.T) {
	g, u, pl := moveGame(t)
	pl.Inventory[0] = wire.Item{Index: 413}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: efAmount, Value: 3}
	pl.Inventory[1] = pl.Inventory[0]
	moveItem(g, u, wire.SlotInv, 0, wire.SlotInv, 1)
	if itemAmount(&pl.Inventory[1]) != 6 || pl.Inventory[0].Index != 0 {
		t.Fatalf("after stacking: %+v %+v", pl.Inventory[0], pl.Inventory[1])
	}
}

func TestStorageOnlyInCity(t *testing.T) {
	g, u, pl := moveGame(t)
	pl.Inventory[0] = wire.Item{Index: 412}
	g.mobs[1].target = wire.Position{X: 1000, Y: 1000}
	moveItem(g, u, wire.SlotInv, 0, wire.SlotStorage, 0)
	if u.account.StorageItems[0].Index != 0 || pl.Inventory[0].Index != 412 {
		t.Fatal("used the storage outside a city")
	}
	z := &g.data.Cities[0]
	g.mobs[1].target = wire.Position{X: uint16(z.CityMinX + 1), Y: uint16(z.CityMinY + 1)}
	moveItem(g, u, wire.SlotInv, 0, wire.SlotStorage, 0)
	if u.account.StorageItems[0].Index != 412 {
		t.Fatal("the storage is closed in the city")
	}
}

func TestEnergizeRune(t *testing.T) {
	g, u, pl := moveGame(t)
	pl.Inventory[0] = wire.Item{Index: runeFirst}
	pl.Inventory[1] = wire.Item{Index: itemEmptyRune}
	moveItem(g, u, wire.SlotInv, 0, wire.SlotInv, 1)
	if pl.Inventory[0].Index != 0 || itemAmount(&pl.Inventory[1]) != 5 {
		t.Fatalf("after: %+v", pl.Inventory[1])
	}
	pl.Inventory[0] = wire.Item{Index: runeFirst}
	moveItem(g, u, wire.SlotInv, 0, wire.SlotInv, 1)
	if pl.Inventory[0].Index != runeFirst || itemAmount(&pl.Inventory[1]) != 5 {
		t.Fatal("the same rune energized twice")
	}
}

func TestGiveTimeSphereAndFairy(t *testing.T) {
	g, u, pl := moveGame(t)
	now := time.Date(2026, 12, 30, 12, 0, 0, 0, time.Local)
	pl.Equip[14] = wire.Item{Index: 3980}
	g.giveTime(u, wire.SlotInv, 0, wire.SlotEquip, 14, now)
	e := pl.Equip[14].Effect
	if e[0].Value != 2 || e[1].Value != 1 || e[2].Value != 27 || e[1].Index != 110 {
		t.Fatalf("sphere: %+v", e)
	}
	pl.Equip[13] = wire.Item{Index: 3900}
	g.giveTime(u, wire.SlotInv, 0, wire.SlotEquip, 13, now)
	if f := pl.Equip[13].Effect[0]; f.Index != 106 || f.Value != 3 {
		t.Fatalf("fairy: %+v", f)
	}
}

func TestFairyExpires(t *testing.T) {
	g, _, pl := moveGame(t)
	pl.Equip[13] = wire.Item{Index: 3901}
	pl.Equip[13].Effect[0] = wire.ItemEffect{Index: 106, Value: 0}
	pl.Equip[13].Effect[1] = wire.ItemEffect{Index: 107, Value: 0}
	pl.Equip[13].Effect[2] = wire.ItemEffect{Index: 108, Value: 2}
	g.fairyMinuteTimer()
	if pl.Equip[13].Index != 3901 || pl.Equip[13].Effect[2].Value != 1 {
		t.Fatalf("after a minute: %+v", pl.Equip[13])
	}
	g.fairyMinuteTimer()
	if pl.Equip[13].Index != 0 {
		t.Fatal("did not expire")
	}
}

func TestCrownOnlyWithCastle(t *testing.T) {
	g, u, pl := moveGame(t)
	pl.GuildIndex = 7
	pl.Inventory[0] = wire.Item{Index: itemCrown}
	moveItem(g, u, wire.SlotInv, 0, wire.SlotStorage, 0)
	if pl.Inventory[0].Index != itemCrown {
		t.Fatal("crown stored without the castle")
	}
	g.chargedGuilds[g.cfg.Channel-1][cityNoatun] = 7
	moveItem(g, u, wire.SlotInv, 0, wire.SlotStorage, 0)
	if u.account.StorageItems[0].Index != itemCrown {
		t.Fatal("the owner could not store the crown")
	}
}

func TestCrackSendsBackToList(t *testing.T) {
	g, u, _ := moveGame(t)
	for range crackLimit {
		g.addCrack(u, 1, "test")
	}
	if u.status != userSelChar || u.crackCount != 0 {
		t.Fatalf("status %d count %d", u.status, u.crackCount)
	}
}

func TestFrostBossGivesHonor(t *testing.T) {
	g, u, pl := moveGame(t)
	u.account.Honra = 5
	g.monsterPrize(genBossFrost, 1)
	found := false
	for _, it := range pl.Inventory[:60] {
		found = found || it.Index != 0
	}
	if u.account.Honra != 25 || !found {
		t.Fatalf("honra %d prize %v", u.account.Honra, found)
	}
}

func TestAlmaEvery500Kills(t *testing.T) {
	g, _, pl := moveGame(t)
	for range almaKills - 1 {
		g.monsterPrize(genAlmaUni, 1)
	}
	if firstSlotOf(pl, 1740) >= 0 {
		t.Fatal("too early")
	}
	g.monsterPrize(genAlmaUni, 1)
	if firstSlotOf(pl, 1740) < 0 {
		t.Fatal("no soul")
	}
}

func TestResurrectAlly(t *testing.T) {
	g := warGame(t, 2)
	u := g.users[1]
	g.mobs[1].target, g.mobs[2].target = wire.Position{X: 2100, Y: 2100}, wire.Position{X: 2101, Y: 2100}
	dead := &g.mobs[2].info.Player.Status
	dead.CurHP, dead.MaxHP = 0, 1000
	p := wire.Attack{}
	p.Header.Size = attackSize1
	var sc skillCast
	// Neither group nor guild: refused.
	if !g.resurrectAlly(u, &p, 0, 2, &sc) || dead.CurHP != 0 {
		t.Fatalf("hp %d", dead.CurHP)
	}
	g.mobs[1].info.Player.GuildIndex, g.mobs[2].info.Player.GuildIndex = 7, 7
	for range 200 {
		if g.resurrectAlly(u, &p, 0, 2, &sc); dead.CurHP != 0 {
			break
		}
	}
	if dead.CurHP == 0 { // the score caps it to the character's real maximum
		t.Fatal("not resurrected")
	}
}

func TestProcAffectOnMob(t *testing.T) {
	g := warGame(t, 1)
	id := firstNPC + 2
	g.mobs[id].mode = modeBattle
	skill := -1
	for i := range g.data.Skills {
		if g.data.Skills[i].TickType > 0 && g.data.Skills[i].Time > 0 {
			skill = i
			break
		}
	}
	if skill < 0 {
		t.Skip("no tick skill in the data")
	}
	if !g.procAffect(id, skill, 100, 300) {
		t.Fatal("proc refused")
	}
	a := g.mobs[id].info.Affects[0]
	if int(a.Index) != g.data.Skills[skill].TickType || a.Master != 255 {
		t.Fatalf("affect %+v", a)
	}
}
