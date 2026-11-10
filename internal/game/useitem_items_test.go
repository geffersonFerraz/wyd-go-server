package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func useItemGame(t *testing.T) (*Game, *User, *wire.Mob) {
	return useItemGameAt(t, 2112, 2042)
}

func useItemGameAt(t *testing.T, x, y int) (*Game, *User, *wire.Mob) {
	g := worldGame(t)
	u := placePlayer(g, x, y, 0)
	return g, u, &g.mobs[1].info.Player
}

func TestColorAndRemover(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.Inventory[0] = wire.Item{Index: 3409}
	if g.data.itemAbility(&pl.Inventory[0], EF_VOLATILE) != 186 {
		t.Skip("3409 is not volatile 186 here")
	}
	pl.Inventory[5] = sanced(1103, 5)
	useOn(g, u, 0, wire.SlotInv, 5)
	if pl.Inventory[5].Effect[0].Index != 118 || itemSanc(&pl.Inventory[5]) != 5 || pl.Inventory[0].Index != 0 {
		t.Fatalf("color: %+v", pl.Inventory[5].Effect[0])
	}
	pl.Inventory[0] = wire.Item{Index: 3417}
	useOn(g, u, 0, wire.SlotInv, 5)
	if pl.Inventory[5].Effect[0].Index != EF_SANC {
		t.Fatalf("remover: %+v", pl.Inventory[5].Effect[0])
	}
}

func TestReplationKeepsRefine(t *testing.T) {
	g, u, pl := useItemGame(t)
	var target uint16
	for id := uint16(1000); int(id) < MaxItemList; id++ {
		d := g.data.Item(id)
		if d.Pos >= 2 && d.Pos <= 32 && g.data.effectValueByIndex(id, EF_UNKNOW1) == 1 && g.data.effectValueByIndex(id, EF_MOBTYPE) == 0 {
			target = id
			break
		}
	}
	if target == 0 {
		t.Skip("no [A] armor")
	}
	pl.Inventory[0] = wire.Item{Index: 4016}
	pl.Inventory[5] = wire.Item{Index: target, Effect: [3]wire.ItemEffect{{Index: 2, Value: 9}, {Index: EF_SANC, Value: 4}}}
	useOn(g, u, 0, wire.SlotInv, 5)
	it := pl.Inventory[5]
	if pl.Inventory[0].Index != 0 || itemSanc(&it) != 4 || it.Effect[0].Index != EF_SANC || it.Effect[1].Index == 0 {
		t.Fatalf("replation: %+v", it.Effect)
	}
}

func TestBagJewelBoxGoldBar(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.Inventory[0] = wire.Item{Index: 3467}
	useSlot(g, u, 0)
	if pl.Inventory[60].Index != 0 || pl.Inventory[0].Index != 3467 {
		t.Fatalf("bag: %+v", pl.Inventory[60])
	}
	pl.Inventory[1] = stack(4528, 2)
	useSlot(g, u, 1)
	if itemAmount(&pl.Inventory[1]) != 1 || inventoryAmount(pl, 2441)+inventoryAmount(pl, 2442)+inventoryAmount(pl, 2443)+inventoryAmount(pl, 2444) != 1 {
		t.Fatal("jewel box")
	}
	pl.Gold = 1999000000
	pl.Inventory[2] = wire.Item{Index: 4011}
	useSlot(g, u, 2)
	if pl.Gold != 1999000000 || pl.Inventory[2].Index != 4011 {
		t.Fatal("gold bar went over 2G")
	}
	pl.Gold = 0
	useSlot(g, u, 2)
	if pl.Gold != 1000000000 || pl.Inventory[2].Index != 0 {
		t.Fatalf("gold bar: %d", pl.Gold)
	}
}

func TestDivinaDateBuff(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.Inventory[0] = wire.Item{Index: 3379}
	useSlot(g, u, 0)
	if left := dateRemaining(u.account.Divina); left < 6.9 || left > 7.1 {
		t.Fatalf("divina days %f", left)
	}
	m := &g.mobs[1]
	slot := affectSlot(m, 34)
	if m.info.Affects[slot].Index != 34 || m.info.Affects[slot].Time != 999 {
		t.Fatalf("affect %+v", m.info.Affects[slot])
	}
	// Ticking does not wear it out while the date is ahead...
	m.info.Affects[slot].Time = 3
	g.processAffect(1)
	if m.info.Affects[slot].Index != 34 || m.info.Affects[slot].Time < 900 {
		t.Fatalf("after tick %+v", m.info.Affects[slot])
	}
	// ...and it goes once the date has passed.
	u.account.Divina = itemDate(time.Now().Add(-time.Hour))
	g.refreshDateBuffs(u)
	if m.info.Affects[slot].Index == 34 {
		t.Fatal("expired divina kept")
	}
}

func TestPardonAndExpChest(t *testing.T) {
	g, u, pl := useItemGame(t)
	setPKPoint(pl, 75-40)
	pl.Inventory[0] = wire.Item{Index: 3343}
	useSlot(g, u, 0)
	if pkPoint(pl) != 75+35 {
		t.Fatalf("pk %d", pkPoint(pl))
	}
	pl.Inventory[1] = wire.Item{Index: 4548}
	useSlot(g, u, 1)
	m := &g.mobs[1]
	if s := affectSlot(m, 39); m.info.Affects[s].Index != 39 || m.info.Affects[s].Time != 225 {
		t.Fatal("exp chest")
	}
}

func TestCelestial90Unlock(t *testing.T) {
	g, u, pl := useItemGame(t)
	m := &g.mobs[1]
	pl.Equip[0].Effect[1].Value = evoCelestial
	pl.BStatus.Level = 89
	m.info.Info |= questLvBlocked
	m.info.Fame = 600
	pl.Inventory[0] = wire.Item{Index: 3020}
	fixRolls(t, 0)
	useSlot(g, u, 0)
	if m.info.Info&questLvBlocked != 0 || m.info.Info&questUnlock89 == 0 || m.info.Fame != 100 || inventoryAmount(pl, 3502) != 1 {
		t.Fatalf("unlock 90: info %x fame %d", m.info.Info, m.info.Fame)
	}
}
