package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestAnctGemOnSet(t *testing.T) {
	g, u, pl := useItemGame(t)
	var armor uint16
	for id := uint16(1000); int(id) < MaxItemList; id++ {
		if g.data.Item(id).Pos == 4 {
			armor = id
			break
		}
	}
	if armor == 0 {
		t.Skip("no armor")
	}
	pl.Equip[4] = wire.Item{Index: armor, Effect: [3]wire.ItemEffect{{Index: EF_SANC, Value: 235}}} // +11, gem 1
	pl.Inventory[0] = wire.Item{Index: 3389}                                                        // gem 3
	useOn(g, u, 0, wire.SlotEquip, 4)
	if pl.Equip[4].Effect[0].Value != 237 || pl.Inventory[0].Index != 0 {
		t.Fatalf("anct: %+v", pl.Equip[4].Effect[0])
	}
	// Same gem again: refused, kept.
	pl.Inventory[0] = wire.Item{Index: 3389}
	useOn(g, u, 0, wire.SlotEquip, 4)
	if pl.Inventory[0].Index != 3389 || pl.Equip[4].Effect[0].Value != 237 {
		t.Fatal("same gem used")
	}
	// Not on the inventory.
	useOn(g, u, 0, wire.SlotInv, 5)
	if pl.Inventory[0].Index != 3389 {
		t.Fatal("gem used on the inventory")
	}
}

func TestStatResetKeepsPoints(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.ClassInfo = 0
	pl.BStatus.Level = 100
	pl.BStatus.STR, pl.BStatus.INT, pl.BStatus.DEX, pl.BStatus.CON = 8+150, 4+3, 7, 6
	g.getCurrentScore(u.id)
	before := pl.StatusPoint
	pl.Inventory[0] = wire.Item{Index: 3220}
	useSlot(g, u, 0)
	if pl.BStatus.STR != 58 || pl.BStatus.INT != 4 || pl.StatusPoint != before+103 || pl.Inventory[0].Index != 0 {
		t.Fatalf("reset: str %d int %d points %d", pl.BStatus.STR, pl.BStatus.INT, pl.StatusPoint)
	}
	// Under 100 in every stat: 3220 refuses, 3265 works.
	pl.Inventory[0] = wire.Item{Index: 3220}
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 3220 {
		t.Fatal("3220 with < 100 points")
	}
	pl.Inventory[1] = wire.Item{Index: 3265}
	useSlot(g, u, 1)
	if pl.BStatus.STR != 8 || pl.StatusPoint != before+153 {
		t.Fatalf("3265: str %d points %d", pl.BStatus.STR, pl.StatusPoint)
	}
}

func TestQuestBoxUsesOnlyWhatFits(t *testing.T) {
	g, u, pl := useItemGame(t)
	pl.BStatus.Level = 50
	pl.Exp = nextLevel[evoMortal][49]
	pl.Inventory[0] = stack(4117, 3)
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 0 || pl.Exp != nextLevel[evoMortal][49]+150000 || pl.Gold != 18000 {
		t.Fatalf("box: exp %d gold %d", pl.Exp-nextLevel[evoMortal][49], pl.Gold)
	}
	// Close to the end of the range: two boxes fill it, one is left.
	pl.BStatus.Level = 112
	pl.Exp = nextLevel[evoMortal][113] - 70000
	pl.Gold = 0
	pl.Inventory[0] = stack(4117, 3)
	useSlot(g, u, 0)
	if itemAmount(&pl.Inventory[0]) != 1 || pl.Exp != nextLevel[evoMortal][113] || pl.Gold != 12000 {
		t.Fatalf("cap: left %d exp %d gold %d", itemAmount(&pl.Inventory[0]), pl.Exp, pl.Gold)
	}
	// Out of the range.
	pl.BStatus.Level = 120
	useSlot(g, u, 0)
	if itemAmount(&pl.Inventory[0]) != 1 {
		t.Fatal("box out of range")
	}
}

func TestMysteryStoneSwapsSub(t *testing.T) {
	g, u, pl := useItemGame(t)
	m := &g.mobs[1]
	if g.data.attribute(int(m.target.X), int(m.target.Y))&attrVillage == 0 {
		found := false
		for x := 2050; x < 2200 && !found; x++ {
			for y := 2000; y < 2150 && !found; y++ {
				if g.data.attribute(x, y)&attrVillage != 0 {
					m.target = wire.Position{X: uint16(x), Y: uint16(y)}
					found = true
				}
			}
		}
		if !found {
			t.Skip("no village around Armia")
		}
	}
	pl.Equip[0] = wire.Item{Index: 1, Effect: [3]wire.ItemEffect{{}, {Index: 7, Value: evoSubCele}}}
	m.info.Sub.Equip[0] = wire.Item{Index: 2, Effect: [3]wire.ItemEffect{{}, {Index: 17, Value: evoCelestial}}}
	pl.Exp, m.info.Sub.Exp = 111, 222
	m.info.Info, m.info.Sub.Info = questElime, questNoas
	pl.SkillBar1 = [4]uint8{1, 2, 3, 4}
	m.info.Sub.SkillBar[0] = 9
	pl.Inventory[0] = wire.Item{Index: 4148}
	useSlot(g, u, 0)
	if pl.Equip[0].Index != 2 || evolution(pl) != evoCelestial || pl.ClassInfo != 1 || pl.Exp != 222 ||
		m.info.Info != questNoas || pl.SkillBar1[0] != 9 || m.info.Sub.SkillBar[0] != 1 || m.info.Sub.Status != 1 {
		t.Fatalf("swap: equip %d evo %d class %d exp %d", pl.Equip[0].Index, evolution(pl), pl.ClassInfo, pl.Exp)
	}
	// Too soon to swap back.
	pl.Inventory[0] = wire.Item{Index: 4148}
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 4148 || pl.Equip[0].Index != 2 {
		t.Fatal("swapped twice in 5 s")
	}
}

func TestJewelsAndPaclac(t *testing.T) {
	g, u, pl := useItemGame(t)
	m := &g.mobs[1]
	pl.Inventory[0] = wire.Item{Index: 3200}
	pl.Inventory[1] = wire.Item{Index: 3201}
	useSlot(g, u, 0)
	useSlot(g, u, 1)
	s := affectSlot(m, 8)
	if m.info.Affects[s].Index != 8 || m.info.Affects[s].Value != 3 || m.info.Affects[s].Time != 450 {
		t.Fatalf("jewels: %+v", m.info.Affects[s])
	}
	pl.Inventory[2] = stack(3447, 3)
	useSlot(g, u, 2)
	if inventoryAmount(pl, 412) != 3 || pl.Inventory[2].Index != 0 {
		t.Fatalf("paclac: %d", inventoryAmount(pl, 412))
	}
}

func TestArchCrystalsInOrder(t *testing.T) {
	g, u, pl := useItemGame(t)
	m := &g.mobs[1]
	pl.Equip[0].Effect[1].Value = evoArch
	pl.BStatus.Level = 380
	pl.Exp = nextLevel[evoArch][379] + 50000000
	pl.Inventory[0] = wire.Item{Index: 4107}
	useSlot(g, u, 0)
	if pl.Inventory[0].Index != 4107 {
		t.Fatal("second crystal before the first")
	}
	pl.Inventory[1] = wire.Item{Index: 4106}
	useSlot(g, u, 1)
	if m.info.Info&questElime == 0 || pl.Inventory[1].Index != 0 || pl.Exp != nextLevel[evoArch][379]-50000000 || pl.BStatus.Level >= 380 {
		t.Fatalf("crystal: info %x level %d", m.info.Info, pl.BStatus.Level)
	}
}

func TestCatalystAndRestorer(t *testing.T) {
	g, u, pl := useItemGame(t)
	fixRolls(t, 0)
	pl.Equip[14] = wire.Item{Index: 2363 - 30}
	pl.Inventory[0] = wire.Item{Index: 3344}
	useOn(g, u, 0, wire.SlotInv, 5) // only the mount slot
	if pl.Inventory[0].Index != 3344 {
		t.Fatal("catalyst used off the mount slot")
	}
	useOn(g, u, 0, wire.SlotEquip, 14)
	if pl.Equip[14].Index != 2363 || mountHP(&pl.Equip[14]) != 5000 || pl.Equip[14].Effect[1].Value != 5 {
		t.Fatalf("catalyst: %+v", pl.Equip[14])
	}
	pl.Equip[14].Effect[1].Value = 59
	pl.Inventory[0] = wire.Item{Index: 3351}
	useOn(g, u, 0, wire.SlotEquip, 14)
	if pl.Equip[14].Effect[1].Value != 60 || pl.Inventory[0].Index != 0 {
		t.Fatalf("restorer: %d", pl.Equip[14].Effect[1].Value)
	}
	pl.Inventory[0] = wire.Item{Index: 3352} // wrong family
	pl.Equip[14].Effect[1].Value = 10
	useOn(g, u, 0, wire.SlotEquip, 14)
	if pl.Inventory[0].Index != 3352 {
		t.Fatal("restorer for another mount")
	}
}
