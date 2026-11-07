package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

// fillInventory leaves only n free slots in the base inventory.
func fillInventory(pl *wire.Mob, n int) {
	for i := range invSlots {
		if pl.Inventory[i].Index == 0 && i >= n {
			pl.Inventory[i] = wire.Item{Index: 1}
		}
	}
}

func TestExtractionKeepsRefine(t *testing.T) {
	g, u, pl := useItemGame(t)
	var helm uint16
	for id := uint16(1000); int(id) < MaxItemList; id++ {
		if g.data.Item(id).Pos == 2 && !extractionBlocked[id] {
			helm = id
			break
		}
	}
	if helm == 0 {
		t.Skip("no helmet")
	}
	pl.Equip[1] = wire.Item{Index: helm, Effect: [3]wire.ItemEffect{{Index: 118, Value: 7}, {Index: EF_DAMAGE, Value: 1}}} // colored +7
	pl.Inventory[0] = wire.Item{Index: 3021, Effect: [3]wire.ItemEffect{{Index: EF_UNKNOW1, Value: 1}, {Index: EF_HP, Value: 40}, {Index: EF_AC, Value: 25}}}
	useSlot(g, u, 0)
	e := pl.Equip[1]
	if e.Index != helm || e.Effect[0] != (wire.ItemEffect{Index: 118, Value: 7}) || e.Effect[1].Index != EF_HP || e.Effect[2].Index != EF_AC || pl.Inventory[0].Index != 0 {
		t.Fatalf("extraction: %+v", e)
	}
	// Sets above +9 are refused.
	setItemSanc(&pl.Equip[1], 10, 0)
	pl.Inventory[0] = wire.Item{Index: 3021, Effect: [3]wire.ItemEffect{{Index: EF_UNKNOW1, Value: 1}}}
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 3021 {
		t.Fatal("extraction on a +10 set")
	}
}

func TestCompoundsAndMolar(t *testing.T) {
	g, u, pl := useItemGame(t)
	m := &g.mobs[1]
	pl.BStatus.Level = 70
	pl.BStatus.Mastery = [4]uint16{60, 10, 0, 0}
	pl.Learn[0] = 0x3 | 1<<26
	pl.Inventory[0] = wire.Item{Index: 4124}
	useSlot(g, u, 0)
	if pl.BStatus.Mastery[0] != 10 || pl.BStatus.Mastery[1] != 10 || pl.Learn[0] != 1<<26 || m.info.Info&questGodBless == 0 || pl.Inventory[0].Index != 0 {
		t.Fatalf("chance: mastery %v learn %x", pl.BStatus.Mastery, pl.Learn[0])
	}
	pl.Inventory[0] = wire.Item{Index: 4124}
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 4124 {
		t.Fatal("compound twice")
	}

	pl.BStatus.Level = 200
	var armor uint16
	for id := uint16(1000); int(id) < MaxItemList; id++ {
		if g.data.Item(id).Pos == 4 {
			armor = id
			break
		}
	}
	pl.Equip[2] = sanced(armor, 1)
	pl.Equip[3] = sanced(armor, 6)
	pl.Inventory[1] = wire.Item{Index: 4122}
	useSlot(g, u, 1)
	if itemSanc(&pl.Equip[2]) != 5 || itemSanc(&pl.Equip[3]) != 6 || m.info.Info&questMolarGargula == 0 {
		t.Fatalf("molar: %d %d", itemSanc(&pl.Equip[2]), itemSanc(&pl.Equip[3]))
	}
}

func TestCashCoinAndGoldCompactor(t *testing.T) {
	g, u, pl := useItemGame(t)
	u.account.Cash = 10
	pl.Inventory[0] = wire.Item{Index: 4535}
	useSlot(g, u, 0)
	if u.account.Cash != 1010 {
		t.Fatalf("cash %d", u.account.Cash)
	}
	pl.Gold = 1500000000
	pl.Inventory[1] = stack(4572, 2)
	fillInventory(pl, 0) // stack of 2, no free slot: refused, nothing taken
	useSlot(g, u, 1)
	if pl.Gold != 1500000000 || itemAmount(&pl.Inventory[1]) != 2 {
		t.Fatal("compactor without room")
	}
	pl.Inventory[2] = wire.Item{}
	useSlot(g, u, 1)
	if pl.Gold != 500000000 || pl.Inventory[2].Index != 4011 || itemAmount(&pl.Inventory[1]) != 1 {
		t.Fatalf("compactor: gold %d", pl.Gold)
	}
}

func TestMerchantBoxChecksRoomFirst(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.Gold = 20000000
	pl.Inventory[0] = stack(4545, 2)
	fillInventory(pl, 0)
	useSlot(g, u, 0)
	if pl.Gold != 20000000 || itemAmount(&pl.Inventory[0]) != 2 {
		t.Fatal("merchant box paid with no room")
	}
	pl.Inventory[5] = wire.Item{}
	fixRolls(t, 0)
	useSlot(g, u, 0)
	if pl.Gold != 10000000 || pl.Inventory[5].Index != 4547 || itemAmount(&pl.Inventory[0]) != 1 {
		t.Fatalf("merchant: gold %d got %d", pl.Gold, pl.Inventory[5].Index)
	}
}

func TestBoxesAndBooks(t *testing.T) {
	g, u, pl := useItemGame(t)
	// Frozen box needs 3 free slots; prize 4522 comes as 3 items.
	pl.Inventory[0] = wire.Item{Index: 4541}
	fillInventory(pl, 3) // slots 0..2 free besides the box in 0 -> 1,2 free
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 4541 {
		t.Fatal("frozen box with 2 free slots")
	}
	pl.Inventory[29] = wire.Item{}
	fixRolls(t, 4)
	useSlot(g, u, 0)
	if inventoryAmount(pl, 4522) != 3 || pl.Inventory[0].Index != 4522 {
		t.Fatalf("frozen: %d", inventoryAmount(pl, 4522))
	}

	// Bone Demon epic: two separate 3338 +4.
	g2, u2, pl2 := useItemGame(t)
	pl2.Inventory[0] = wire.Item{Index: 4724}
	fixRolls(t, 99, 1)
	useSlot(g2, u2, 0)
	if inventoryAmount(pl2, 3338) != 2 || itemSanc(&pl2.Inventory[0]) != 4 {
		t.Fatalf("bone: %d", inventoryAmount(pl2, 3338))
	}

	// Weapon box D: a damage weapon with EF_SANC + damage.
	pl2.Inventory[5] = wire.Item{Index: 4631}
	fixRolls(t, 0)
	useSlot(g2, u2, 5)
	if w := pl2.Inventory[5]; w.Index != 824 || w.Effect[1] != (wire.ItemEffect{Index: EF_DAMAGE, Value: 54}) {
		t.Fatalf("weapon box: %+v", w)
	}

	// Lucky egg: an adult mount with 5000 HP.
	pl2.Inventory[6] = wire.Item{Index: 4615}
	useSlot(g2, u2, 6)
	if pl2.Inventory[6].Index != 2360 || mountHP(&pl2.Inventory[6]) != 5000 {
		t.Fatalf("egg: %+v", pl2.Inventory[6])
	}
}

func TestReplationImpKeepsHighRefine(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.Inventory[5] = sanced(753, 11)
	pl.Inventory[0] = wire.Item{Index: 4686}
	fixRolls(t, 0)
	useOn(g, u, 0, wire.SlotInv, 5)
	it := pl.Inventory[5]
	if itemSanc(&it) != 11 || it.Effect[1].Index != EF_DAMAGE || it.Effect[2].Index != EF_NOTRADE || pl.Inventory[0].Index != 0 {
		t.Fatalf("imp: sanc %d %+v", itemSanc(&it), it.Effect)
	}
	pl.Inventory[0] = wire.Item{Index: 4687} // Griupan replation on an Imp
	useOn(g, u, 0, wire.SlotInv, 5)
	if pl.Inventory[0].Index != 4687 {
		t.Fatal("griupan replation on an imp")
	}
}

func TestMissionPassAndEscrituraAccelerator(t *testing.T) {
	g, u, pl := useItemGame(t)
	m := &g.mobs[1]
	pl.Inventory[0] = wire.Item{Index: 4617}
	useSlot(g, u, 0)
	if left := dateRemaining(m.info.DailyQuest.BattlePass); left < 6.9 || left > 7.1 {
		t.Fatalf("pass days %f", left)
	}
	pl.Inventory[0] = wire.Item{Index: 4621}
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 4621 {
		t.Fatal("second pass while one is active")
	}

	pl.Equip[0].Effect[1].Value = evoCelestial
	used := time.Now().Add(-time.Hour)
	m.info.Escritura = itemDate(used)
	pl.Inventory[1] = wire.Item{Index: 4682}
	useSlot(g, u, 1)
	e := m.info.Escritura
	got := time.Date(int(e.Ano), time.Month(e.Mes), int(e.Dia), int(e.Hora), int(e.Minuto), int(e.Segundo), 0, time.Local)
	if d := used.Sub(got); d < 10*time.Hour-time.Second || d > 10*time.Hour+time.Second || pl.Inventory[1].Index != 0 {
		t.Fatalf("accelerator moved %v", d)
	}
}

func TestChangeClassRelogs(t *testing.T) {
	g, u, pl := useItemGame(t)
	listen(u)
	u.charSlot = 2
	pl.Equip[0] = wire.Item{Index: 26, Effect: [3]wire.ItemEffect{{}, {Index: 26, Value: evoArch}}} // TK body 26
	pl.ClassInfo = 0
	pl.Equip[11] = wire.Item{Index: 1762} // BeastMaster sephirot
	pl.Inventory[0] = wire.Item{Index: 3486}
	useSlot(g, u, 0)
	if pl.ClassInfo != 2 || pl.Equip[0].Index != 28 || pl.Equip[0].Effect[1].Index != 28 || pl.Equip[11].Index != 0 || u.status != userSelChar {
		t.Fatalf("class: %d face %d status %d", pl.ClassInfo, pl.Equip[0].Index, u.status)
	}
	if len(sent(u, opSrvOutSealSlot)) == 0 {
		t.Error("no 0x3B4 to re-enter the character")
	}
}

func TestBlessedImago(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.Equip[14] = wire.Item{Index: 2370, Effect: [3]wire.ItemEffect{{}, {Index: 120, Value: 10}}}
	pl.Inventory[0] = wire.Item{Index: 3231} // wrong imago
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 3231 {
		t.Fatal("wrong imago used")
	}
	pl.Inventory[0] = wire.Item{Index: 3230}
	fixRolls(t, 0)
	useSlot(g, u, 0)
	if pl.Equip[14].Effect[1].Index != 121 || pl.Inventory[0].Index != 0 {
		t.Fatalf("imago: level %d", pl.Equip[14].Effect[1].Index)
	}
}
