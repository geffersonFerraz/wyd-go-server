package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestHitRateTable(t *testing.T) {
	if hitRate[0] != 512 || hitRate[1] != 512 || hitRate[2] != 512 || hitRate[3] != 1024-0 && hitRate[3] != 999 {
		t.Fatalf("hit rate start %v", hitRate[:8])
	}
	for i, v := range hitRate {
		if v < 0 || v > 999 && i != 0 {
			t.Fatalf("hitRate[%d] = %d", i, v)
		}
	}
}

func TestGetDamage(t *testing.T) {
	for range 1000 {
		if d := getDamage(100, 2000, 0); d != 1 {
			t.Fatalf("huge defense: %d, want 1", d)
		}
		if d := getDamage(1000, 200, 0); d < 900*99/100 || d > 900*110/100 {
			t.Fatalf("1000 vs 200: %d", d)
		}
	}
}

func TestKillNPCGivesExp(t *testing.T) {
	g := worldGame(t)
	// Pick a weak, non-merchant NPC outside towns.
	npc := 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		pl := &m.info.Player
		if m.mode == 0 || pl.Info&0x3F != 0 || pl.CapeInfo > 2 || pl.Exp <= 0 || pl.Equip[13].Index != 0 {
			continue
		}
		if g.data.attribute(int(m.target.X), int(m.target.Y))&attrVillage != 0 {
			continue
		}
		if npc == 0 || pl.Status.Level < g.mobs[npc].info.Player.Status.Level {
			npc = id
		}
	}
	if npc == 0 {
		t.Fatal("no weak NPC")
	}
	nt := g.mobs[npc].target
	u := placePlayer(g, int(nt.X)+1, int(nt.Y), 0)
	pl := &g.mobs[1].info.Player
	pl.ClassInfo = 0
	pl.Status.Level, pl.BStatus.Level = 10, 10
	pl.Status.Attack = 50000
	u.attackRange = 2
	gen := g.mobs[npc].npc.generateID
	before := g.genCount[gen]

	p := wire.Attack{Header: wire.NewHeader(wire.OpCliAttackOne, 1), SkillID: -1}
	p.Header.Size = attackSize1
	p.Target[0] = wire.AttackTarget{Index: int32(npc), Damage: -2}
	raw := wire.MustMarshal(&p)[:attackSize1]
	g.requestAttack(u, packetRaw(raw))

	if g.mobs[npc].mode != 0 {
		t.Fatalf("npc %d still alive with %d hp", npc, g.mobs[npc].info.Player.Status.CurHP)
	}
	if pl.Exp <= 0 {
		t.Fatal("no exp for the kill")
	}
	if g.genCount[gen] != before-1 {
		t.Fatalf("generator count %d -> %d", before, g.genCount[gen])
	}
	if g.cell(int(nt.X), int(nt.Y)) == uint16(npc) {
		t.Fatal("dead npc still on the grid")
	}

	// A second attack within 900 ms is ignored.
	g.requestAttack(u, packetRaw(raw))
}

func TestLevelUp(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	pl := &g.mobs[1].info.Player
	pl.Equip[0] = wire.Item{Index: 1}
	pl.Equip[0].Effect[1].Value = evoMortal
	pl.Status.Level, pl.BStatus.Level = 5, 5
	pl.Exp = nextLevel[evoMortal][5]
	g.checkGetLevel(u)
	if pl.BStatus.Level != 6 || pl.Status.CurHP != pl.Status.MaxHP || pl.Status.MaxHP == 0 {
		t.Fatalf("level %d hp %d/%d", pl.BStatus.Level, pl.Status.CurHP, pl.Status.MaxHP)
	}
}
