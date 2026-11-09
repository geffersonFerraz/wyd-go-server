package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// window builds a compounder packet from inventory slots (-1 = empty).
func window(g *Game, u *User, op uint16, slots ...int) wire.Compor {
	pl := &g.mobs[u.id].info.Player
	p := wire.Compor{Header: wire.NewHeader(op, uint16(u.id))}
	for i := range p.Slot {
		p.Slot[i] = -1
	}
	for i, s := range slots {
		p.Slot[i] = int8(s)
		if s >= 0 {
			p.Items[i] = pl.Inventory[s]
		}
	}
	return p
}

func stack(id uint16, n int) wire.Item {
	it := wire.Item{Index: id}
	if n > 1 {
		it.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint16(n)}
	}
	return it
}

func sanced(id uint16, sanc int) wire.Item {
	it := wire.Item{Index: id}
	setItemSanc(&it, sanc, 0)
	return it
}

func TestComporWindowChecks(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	for i := range 7 {
		pl.Inventory[i] = stack(413, 1)
	}
	// Claiming an item that is not there: nothing is taken.
	p := window(g, u, opCliOdin, 0, 1, 2, 3, 4, 5, 6)
	p.Items[3] = wire.Item{Index: 5134}
	g.requestOdin(u, packet(&p))
	// Same slot twice.
	p = window(g, u, opCliOdin, 0, 1, 2, 3, 4, 5, 5)
	g.requestOdin(u, packet(&p))
	for i := range 7 {
		if pl.Inventory[i].Index != 413 {
			t.Fatalf("slot %d taken by a bad window", i)
		}
	}
	// The real 7 Lactolerium make a Pista de Runas.
	p = window(g, u, opCliOdin, 0, 1, 2, 3, 4, 5, 6)
	g.requestOdin(u, packet(&p))
	if pl.Inventory[0].Index != 5134 || pl.Inventory[6].Index != 0 {
		t.Fatalf("runes: %v %v", pl.Inventory[0], pl.Inventory[6])
	}
}

func TestShanyChecksTheLac(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Equip[0].Effect[1].Value = evoArch
	pl.BStatus.Level = 300
	pl.Inventory[0], pl.Inventory[1], pl.Inventory[2] = sanced(540, 9), sanced(541, 9), wire.Item{Index: 540}
	// The C++ never compared places 3..6 with the inventory.
	p := window(g, u, opCliShany, 0, 1, 2, 10, 11, 12, 13)
	for i := 3; i < 7; i++ {
		p.Items[i] = wire.Item{Index: 413}
	}
	g.requestShany(u, packet(&p))
	if pl.Inventory[2].Index != 540 {
		t.Fatal("Shany ran with Lactolerium the player does not have")
	}
	for i := 10; i < 14; i++ {
		pl.Inventory[i] = wire.Item{Index: 413}
	}
	pl.Exp = 10000000000
	fixRolls(t, 99, 0)
	p = window(g, u, opCliShany, 0, 1, 2, 10, 11, 12, 13)
	g.requestShany(u, packet(&p))
	if id := pl.Inventory[2].Index; id != 631 || pl.Inventory[0].Index != 0 || pl.Inventory[10].Index != 0 {
		t.Fatalf("Shany result %d", id)
	}
}

func TestAnct(t *testing.T) {
	g := worldGame(t)
	var weapon uint16
	for id := uint16(1); int(id) < MaxItemList; id++ {
		d := g.data.Item(id)
		if d.Unique >= 41 && d.Unique <= 49 && d.Extreme > 0 && d.Pos > 0 {
			weapon = id
			break
		}
	}
	if weapon == 0 {
		t.Skip("no weapon with an Ancient")
	}
	armor := refineTarget(t, g)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = sanced(weapon, 9)
	pl.Inventory[1] = wire.Item{Index: 2442}
	for i := 2; i < 8; i++ {
		pl.Inventory[i] = sanced(armor, 9)
	}
	fixRolls(t, 0)
	p := window(g, u, opCliCompounder, 0, 1, 2, 3, 4, 5, 6, 7)
	g.requestCompounder(u, packet(&p))
	want := uint16(int(g.data.Item(weapon).Extreme) + 1)
	if pl.Inventory[0].Index != want || itemSanc(&pl.Inventory[0]) != 7 || pl.Inventory[7].Index != 0 {
		t.Fatalf("anct: %+v, want %d +7", pl.Inventory[0], want)
	}
}

func TestEhreEarringsNeedEarrings(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = wire.Item{Index: 591}
	pl.Inventory[1] = wire.Item{Index: 1103} // not an earring
	pl.Inventory[2] = wire.Item{Index: 592}
	p := window(g, u, opCliEhre, 0, 1, 2)
	g.requestEhre(u, packet(&p))
	if pl.Inventory[1].Index != 1103 {
		t.Fatal("non-earring eaten")
	}
	pl.Inventory[1] = wire.Item{Index: 593}
	fixRolls(t, 0)
	p = window(g, u, opCliEhre, 0, 1, 2)
	g.requestEhre(u, packet(&p))
	if pl.Inventory[0].Index != 4719 || itemAmount(&pl.Inventory[0]) != 3 || pl.Inventory[2].Index != 0 {
		t.Fatalf("earring dust: %+v", pl.Inventory[0])
	}
}

func TestEhreSoul(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0], pl.Inventory[1], pl.Inventory[2] = wire.Item{Index: 2441}, wire.Item{Index: 2443}, wire.Item{Index: 2442}
	p := window(g, u, opCliEhre, 0, 1, 2)
	g.requestEhre(u, packet(&p))
	if g.mobs[1].info.Soul != 10 || pl.Inventory[0].Index != 0 {
		t.Fatalf("soul %d", g.mobs[1].info.Soul)
	}
}

func TestOdinRefinePlus11(t *testing.T) {
	g := worldGame(t)
	armor := refineTarget(t, g)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0], pl.Inventory[1] = stack(413, 10), stack(413, 10)
	pl.Inventory[2] = sanced(armor, 11)
	for i := 3; i < 7; i++ {
		pl.Inventory[i] = wire.Item{Index: 3338}
	}
	fixRolls(t, 0)
	p := window(g, u, opCliOdin, 0, 1, 2, 3, 4, 5, 6)
	g.requestOdin(u, packet(&p))
	if itemSanc(&pl.Inventory[2]) != 12 || pl.Inventory[0].Index != 0 || pl.Inventory[3].Index != 0 {
		t.Fatalf("odin: sanc %d", itemSanc(&pl.Inventory[2]))
	}
	// A failure takes one level off (roll 49 is a failure but not a break: 49%5 != 0).
	pl.Inventory[0], pl.Inventory[1] = stack(413, 10), stack(413, 10)
	for i := 3; i < 7; i++ {
		pl.Inventory[i] = wire.Item{Index: 3338}
	}
	fixRolls(t, 49)
	p = window(g, u, opCliOdin, 0, 1, 2, 3, 4, 5, 6)
	g.requestOdin(u, packet(&p))
	if itemSanc(&pl.Inventory[2]) != 11 {
		t.Fatalf("odin failure: sanc %d", itemSanc(&pl.Inventory[2]))
	}
}

func TestArchLevelBlockAndLindy(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	m := &g.mobs[1]
	pl := &m.info.Player
	pl.Equip[0].Effect[1].Value = evoArch
	pl.Status.Level, pl.BStatus.Level = 353, 353
	pl.Exp = nextLevel[evoArch][353] + 5000
	pl.CapeInfo = 7
	g.checkGetLevel(u)
	if pl.BStatus.Level != 354 || m.info.Info&questLvBlockedArch == 0 || pl.Exp != nextLevel[evoArch][353] {
		t.Fatalf("not blocked at 354: level %d exp %d", pl.BStatus.Level, pl.Exp)
	}
	if g.expApply2(1000, 1, 1, false) != 0 {
		t.Fatal("blocked character still gets exp")
	}
	ids := []uint16{413, 413, 4127, 413, 413, 413, 413}
	for i, id := range ids {
		pl.Inventory[i] = wire.Item{Index: id}
	}
	pl.Inventory[0], pl.Inventory[1] = stack(413, 10), stack(413, 10)
	pl.CapeInfo = 7 // the score refresh on level-up recomputes it
	p := window(g, u, opCliLindy, 0, 1, 2, 3, 4, 5, 6)
	g.requestLindy(u, packet(&p))
	if m.info.Info&questLvBlockedArch != 0 || m.info.Info&questUnlock354 == 0 || pl.Equip[15].Index != 3191 {
		t.Fatalf("lindy: info %x cape %d", m.info.Info, pl.Equip[15].Index)
	}
	if g.expApply2(1000, 1, 1, false) == 0 { // an Arch outside the exp zones gets 20
		t.Fatal("no exp after unlocking")
	}
}

func TestAlchemy(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Gold = 30000000
	pl.Inventory[0], pl.Inventory[1], pl.Inventory[2] = wire.Item{Index: 413}, wire.Item{Index: 2441}, wire.Item{Index: 2442}
	fixRolls(t, 0)
	p := window(g, u, opCliAlchemy, 0, 1, 2)
	g.requestAlchemy(u, packet(&p))
	if pl.Inventory[0].Index != 3200 || pl.Gold != 10000000 || pl.Inventory[2].Index != 0 {
		t.Fatalf("alchemy: %v gold %d", pl.Inventory[0], pl.Gold)
	}
}

func TestExtraction(t *testing.T) {
	g := worldGame(t)
	var armor uint16
	for id := uint16(1000); int(id) < MaxItemList; id++ {
		d := g.data.Item(id)
		if (d.Pos == 2 || d.Pos == 4 || d.Pos == 8 || d.Pos == 16 || d.Pos == 32) && g.data.effectValueByIndex(id, EF_UNKNOW1) > 0 {
			armor = id
			break
		}
	}
	if armor == 0 {
		t.Skip("no armor with a class")
	}
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Inventory[0] = sanced(armor, 5)
	pl.Inventory[1] = wire.Item{Index: 1774}
	fixRolls(t, 0)
	g.requestExtraction(u, packet(&wire.Extraction{Header: wire.NewHeader(opCliExtraction, 1), SlotID: 0}))
	want := itemExtractionType(int(g.data.Item(armor).Pos))
	if pl.Inventory[0].Index != want || pl.Inventory[0].Effect[0].Index != 87 || pl.Inventory[1].Index != 0 {
		t.Fatalf("extraction: %+v", pl.Inventory[0])
	}
	// A failure destroys the whole item.
	pl.Inventory[0] = sanced(armor, 5)
	pl.Inventory[0].Effect[1] = wire.ItemEffect{Index: 2, Value: 9}
	pl.Inventory[1] = wire.Item{Index: 1774}
	fixRolls(t, 99)
	g.requestExtraction(u, packet(&wire.Extraction{Header: wire.NewHeader(opCliExtraction, 1), SlotID: 0}))
	if pl.Inventory[0] != (wire.Item{}) {
		t.Fatalf("failed extraction left %+v", pl.Inventory[0])
	}
}

// The belts 5706..5725 are not costumes.
func TestEhreRefusesBeltsAsCostume(t *testing.T) {
	for _, belt := range []uint16{5706, 5714, 5718, 5721, 5725} {
		g := worldGame(t)
		u := placePlayer(g, 2112, 2042, 0)
		pl := &g.mobs[1].info.Player
		pl.Gold = 5_000_000
		pl.Inventory[0] = wire.Item{Index: 2361}
		pl.Inventory[1] = wire.Item{Index: belt}
		pl.Inventory[2] = wire.Item{Index: 412}
		p := window(g, u, opCliEhre, 0, 1, 2)
		g.requestEhre(u, packet(&p))
		if pl.Inventory[1].Index != belt || pl.Gold != 5_000_000 {
			t.Fatalf("belt %d eaten: item %d, gold %d", belt, pl.Inventory[1].Index, pl.Gold)
		}
	}
}

// A belt in the accessory slot counts for the character's attributes.
func TestArcaneBeltCountsInAttributes(t *testing.T) {
	g := worldGame(t)
	var mob wire.Mob
	mob.Equip[17] = wire.Item{Index: 5715} // EF_HPADD 20, EF_AC 80
	if got := g.data.mobAbility(&mob, EF_HPADD); got != 20 {
		t.Fatalf("EF_HPADD %d", got)
	}
}
