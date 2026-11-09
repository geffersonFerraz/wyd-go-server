package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// questNPC puts an NPC with the given Info byte and levels next to player 1.
func questNPC(g *Game, info uint8, level, bLevel int) int {
	id := MaxMob - 1
	m := &g.mobs[id]
	*m = Mob{mode: 1}
	m.info.Player.Info = info
	m.info.Player.Status.Level = int16(level)
	m.info.Player.BStatus.Level = int16(bLevel)
	copy(m.info.Player.Name[:], "Teste")
	t := g.mobs[1].target
	m.target = wire.Position{X: t.X + 1, Y: t.Y}
	return id
}

func clickNPC(g *Game, u *User, npc int, click uint32) {
	g.requestMerchantNPC(u, packet(&MerchantClick{Header: wire.NewHeader(opCliMerchantNPC, uint16(u.id)), NPCID: uint32(npc), Click: click}))
}

func fixRand(t *testing.T, v int) {
	old := rnd
	rnd = func() int { return v }
	t.Cleanup(func() { rnd = old })
}

func TestLoadNPCEvents(t *testing.T) {
	d, s := Data{}, RealSettings(t)
	d.applyWorldTables(&s)
	ev := d.NPCEvents[0]
	if ev.ItemRequired[0].Index != 4725 || ev.AmountRequired[0] != 1 || ev.Rates != [10]int{75, 150, 225, 300} {
		t.Fatalf("npc I: %+v", ev)
	}
	if ev.ItemEarned[3][0].Index != 4726 || ev.Name == "" {
		t.Fatalf("npc I prizes: %+v name %q", ev.ItemEarned[3], ev.Name)
	}
	if it := d.NPCEvents[1].ItemEarned[1][0]; it.Index != 413 || it.Effect[0] != (wire.ItemEffect{Index: EF_AMOUNT, Value: 3}) {
		t.Fatalf("npc II item with effects: %+v", it)
	}
	if ev := d.NPCEvents[3]; ev.GoldRequired != 100_000_000 || ev.ItemEarned[0][1].Index != 4043 {
		t.Fatalf("npc IV: gold %d, %+v", ev.GoldRequired, ev.ItemEarned[0])
	}
	if it := d.NPCEvents[7].ItemEarned[0][0]; it.Index != 3026 || it.Effect[2] != (wire.ItemEffect{Index: 4, Value: 120}) {
		t.Fatalf("npc VI 7-field item: %+v", it)
	}
}

func TestNPCEventExchange(t *testing.T) {
	g := worldGame(t)
	if g.data.NPCEvents[0].ItemRequired[0].Index == 0 {
		t.Skip("no NPCEvento.txt")
	}
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	npc := questNPC(g, 0, 500, 1)

	clickNPC(g, u, npc, 1)
	if firstSlotOf(pl, 413) >= 0 {
		t.Fatal("prize without the skull")
	}
	pl.Inventory[0] = wire.Item{Index: 4725}
	fixRand(t, 10) // roll 10 -> tier 0 (rate 75): item 413
	clickNPC(g, u, npc, 1)
	if pl.Inventory[0].Index == 4725 {
		t.Fatal("skull not taken")
	}
	if firstSlotOf(pl, 413) < 0 {
		t.Fatalf("no prize: %+v", pl.Inventory[:4])
	}
}

func TestKingdomBrokerAndSkillMaster(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.CapeInfo = capeBlue
	pl.Equip[15] = wire.Item{Index: 1766}
	pl.Inventory[0] = wire.Item{Index: itemSapphirePack}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 2}
	pl.Inventory[1] = wire.Item{Index: itemSapphire}
	pl.Inventory[1].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 15}
	npc := questNPC(g, 8, 374, 1)
	clickNPC(g, u, npc, 1)
	if pl.Equip[15].Index != 572 {
		t.Fatalf("cape %d, want white 572", pl.Equip[15].Index)
	}
	if sapphiresHeld(pl) != 35-30 {
		t.Fatalf("sapphires left %d", sapphiresHeld(pl))
	}

	// Mestre das Habilidades: 30 sapphires, 100 points of each stat back.
	g.cfg.StatSapphire = 30
	pl.ClassInfo = 0
	pl.BStatus.Level, pl.Status.Level = 300, 300 // enough points for the stats below
	pl.BStatus.STR = uint16(baseSIDCHM[0][0] + 150)
	pl.BStatus.INT = uint16(baseSIDCHM[0][1] + 20)
	pl.Inventory[1] = wire.Item{Index: itemSapphire}
	pl.Inventory[1].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 40}
	npc = questNPC(g, 8, 130, 1)
	clickNPC(g, u, npc, 1)
	if pl.BStatus.STR != uint16(baseSIDCHM[0][0]+50) || pl.BStatus.INT != uint16(baseSIDCHM[0][1]) {
		t.Fatalf("stats after reset: STR %d INT %d", pl.BStatus.STR, pl.BStatus.INT)
	}
	if got := inventoryAmount(pl, itemSapphire); got != 10 {
		t.Fatalf("loose sapphires left %d, want 10", got)
	}
}

func TestSephirotAndUnicorn(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 40_000_000)
	pl := &g.mobs[1].info.Player
	npc := questNPC(g, 19, 0, 1)
	g.mobs[npc].info.Player.Exp = 2
	pl.Equip[10] = wire.Item{Index: 1742}
	for i := range 8 {
		pl.Inventory[i] = wire.Item{Index: uint16(1744 + i)}
	}
	clickNPC(g, u, npc, 0) // asks first
	if pl.Gold != 40_000_000 {
		t.Fatal("charged on the first click")
	}
	clickNPC(g, u, npc, 1)
	if pl.Gold != 10_000_000 || pl.Inventory[0].Index != 1762 || pl.Inventory[7].Index != 0 {
		t.Fatalf("sephirot: gold %d inv %+v", pl.Gold, pl.Inventory[:8])
	}

	// Unicórnio: a +6 917 becomes a +3 939, once.
	npc = questNPC(g, 8, 55, 1)
	w := wire.Item{Index: 917}
	w.Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 6}
	pl.Equip[6] = w
	clickNPC(g, u, npc, 1)
	if pl.Equip[6].Index != 939 || itemSanc(&pl.Equip[6]) != 3 {
		t.Fatalf("unicorn: %+v", pl.Equip[6])
	}
	pl.Equip[6] = w
	clickNPC(g, u, npc, 1)
	if pl.Equip[6].Index != 917 {
		t.Fatal("unicorn helped twice")
	}
}

func TestShamaResetsTree(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Learn[0] = 0xFFFFFF
	pl.BStatus.Mastery = [4]uint16{80, 30, 0, 60}
	pl.Equip[13] = wire.Item{Index: 449} // tree 1
	npc := questNPC(g, 13, 0, 1)
	clickNPC(g, u, npc, 1)
	if pl.Learn[0] != 0xFF00FF {
		t.Fatalf("learn %x, want tree 1 cleared", pl.Learn[0])
	}
	if pl.BStatus.Mastery != [4]uint16{30, 0, 0, 10} || pl.Equip[13].Index != 0 || pl.QuestInfo&pqReset50 == 0 {
		t.Fatalf("mastery %v circle %d quest %x", pl.BStatus.Mastery, pl.Equip[13].Index, pl.QuestInfo)
	}
	pl.Equip[13] = wire.Item{Index: 448}
	clickNPC(g, u, npc, 1)
	if pl.Equip[13].Index != 448 {
		t.Fatal("reset 50 done twice")
	}
}

func TestSetItemBonusLevel(t *testing.T) {
	g := worldGame(t)
	// A weapon (Pos 64/192) gets its three effects filled.
	var weapon uint16
	for id := uint16(1); int(id) < MaxItemList; id++ {
		if d := g.data.Item(id); (d.Pos == 64 || d.Pos == 192) && d.Level > 0 && d.Level < 100 {
			weapon = id
			break
		}
	}
	if weapon == 0 {
		t.Skip("no weapon")
	}
	for range 200 {
		it := wire.Item{Index: weapon}
		g.setItemBonusLevel(&it, int(g.data.Item(weapon).Level)+50, 1, 0)
		for e := range 3 {
			if it.Effect[e].Index == 0 {
				t.Fatalf("effect %d empty: %+v", e, it)
			}
		}
	}
}
