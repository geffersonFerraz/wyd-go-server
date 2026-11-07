package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func summonsOf(g *Game, id int) []int {
	var out []int
	for _, e := range g.mobs[id].evoca {
		if e != 0 {
			out = append(out, e)
		}
	}
	return out
}

func TestEvocation(t *testing.T) {
	g := worldGame(t)
	if g.data.NPCBase[0].Equip[0].Index == 0 {
		t.Skip("no npc_base")
	}
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.ClassInfo = 2
	pl.Status.Mastery[2] = 120 // 120/30 = 4 of the first evocation
	pl.Status.INT = 500

	g.castEvocation(u, firstEvocationSkill)
	list := summonsOf(g, 1)
	if len(list) != 4 {
		t.Fatalf("%d summons, want 4", len(list))
	}
	for _, id := range list {
		s := &g.mobs[id]
		if s.mode != modeNPCIdle || s.npc.routeType != 5 || s.npc.summoner != 1 || s.info.Player.CapeInfo != 4 {
			t.Fatalf("summon %d: mode %d route %d summoner %d cape %d", id, s.mode, s.npc.routeType, s.npc.summoner, s.info.Player.CapeInfo)
		}
		if s.info.Affects[0].Index != affSummoned || s.info.Affects[0].Time != 30 {
			t.Fatalf("summon lifetime: %+v", s.info.Affects[0])
		}
		if s.info.Player.Status.MaxHP <= g.data.NPCBase[0].BStatus.MaxHP || g.cell(int(s.target.X), int(s.target.Y)) != uint16(id) {
			t.Fatalf("summon HP %d / grid", s.info.Player.Status.MaxHP)
		}
	}
	if len(sent(u, opSrvPartyEvocation)) == 0 {
		t.Fatal("no 0x3EA")
	}
	g.castEvocation(u, firstEvocationSkill) // already complete
	g.castEvocation(u, firstEvocationSkill+1)
	if n := len(summonsOf(g, 1)); n != 4 {
		t.Fatalf("recast changed the summons: %d", n)
	}

	// Following: walk away, teleport beyond 20 cells.
	s := list[0]
	g.mobs[1].target.X += 8
	if r := g.standingBy(s); r&aiMove == 0 {
		t.Fatalf("summon did not follow: %x", r)
	}
	g.mobs[1].target.X += 30
	if r := g.standingBy(s); r&aiTeleport == 0 {
		t.Fatalf("summon did not teleport: %x", r)
	}
	g.mobs[1].target.X -= 38

	// Lifetime: affect 24 runs out -> the summon leaves the list.
	g.mobs[s].info.Affects[0].Time = 1
	g.processAffect(s)
	if r := g.standingBy(s); r&aiKeepAway == 0 {
		t.Fatalf("expired summon kept: %x", r)
	}
	g.deleteMob(s, 3)
	if n := len(summonsOf(g, 1)); n != 3 {
		t.Fatalf("expired summon still listed (%d)", n)
	}

	// Logout dismisses the rest.
	g.dismissSummons(1)
	for _, id := range list[1:] {
		if g.mobs[id].mode != 0 {
			t.Fatalf("summon %d left behind", id)
		}
	}
}

func TestBabyMountSummon(t *testing.T) {
	g := worldGame(t)
	if g.data.NPCBase[11].Equip[0].Index == 0 {
		t.Skip("no npc_base")
	}
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.Equip[14] = wire.Item{Index: 2331}
	setMountHP(&pl.Equip[14], 300)
	pl.Equip[14].Effect[1] = wire.ItemEffect{Index: 10, Value: 50} // level 10
	g.mountProcess(1)
	list := summonsOf(g, 1)
	if len(list) != 1 {
		t.Fatalf("%d mount summons", len(list))
	}
	s := &g.mobs[list[0]]
	if !isMountFace(s.info.Player.Equip[0].Index) || s.info.Player.Status.CurHP != 300 {
		t.Fatalf("mount summon face %d hp %d", s.info.Player.Equip[0].Index, s.info.Player.Status.CurHP)
	}
	// A hit on the summon writes the HP back to the item.
	s.info.Player.Status.CurHP = 120
	g.linkMountHP(list[0])
	if mountHP(&pl.Equip[14]) != 120 {
		t.Fatalf("item hp %d", mountHP(&pl.Equip[14]))
	}
	// Re-running replaces it; unequipping removes it.
	g.mountProcess(1)
	if l := summonsOf(g, 1); len(l) != 1 || g.mobs[l[0]].mode == 0 || g.mobs[l[0]].info.Player.Status.CurHP != 120 {
		t.Fatalf("mount not re-summoned with its HP: %v", l)
	}
	pl.Equip[14] = wire.Item{}
	g.mountProcess(1)
	if l := summonsOf(g, 1); len(l) != 0 {
		t.Fatalf("mount summon left: %v", l)
	}
}

func TestAdultMountDamage(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.Equip[14] = wire.Item{Index: 2361}
	setMountHP(&pl.Equip[14], 100)
	pl.Equip[14].Effect[1].Index = 10
	pl.Equip[14].Effect[2].Index = 50 // fed
	g.processAdultMount(1, 30)
	if mountHP(&pl.Equip[14]) != 70 {
		t.Fatalf("hp %d after 30", mountHP(&pl.Equip[14]))
	}
	g.processAdultMount(1, 500)
	if mountHP(&pl.Equip[14]) > 0 || pl.Equip[14].Effect[2].Index != 0 {
		t.Fatalf("mount alive: %+v", pl.Equip[14])
	}
	// Poção Revigorante (affect 49) protects it.
	setMountHP(&pl.Equip[14], 100)
	g.mobs[1].info.Affects[3] = wire.Affect{Index: 49, Time: 10}
	g.processAdultMount(1, 50)
	if mountHP(&pl.Equip[14]) != 100 {
		t.Fatal("mount hurt under affect 49")
	}
}

func TestSummonKillCreditsOwner(t *testing.T) {
	g := worldGame(t)
	if g.data.NPCBase[0].Equip[0].Index == 0 {
		t.Skip("no npc_base")
	}
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.ClassInfo = 2
	pl.Status.Mastery[2] = 30
	g.castEvocation(u, firstEvocationSkill)
	list := summonsOf(g, 1)
	if len(list) == 0 {
		t.Fatal("no summon")
	}
	victim := questNPC(g, 0, 10, 10)
	g.mobs[victim].info.Player.Exp = 1000
	g.mobs[victim].info.Player.CapeInfo = 0
	g.setCell(int(g.mobs[victim].target.X), int(g.mobs[victim].target.Y), uint16(victim))
	before := pl.Exp
	g.mobKilled(victim, list[0])
	if pl.Exp <= before {
		t.Fatalf("owner exp %d -> %d", before, pl.Exp)
	}
	if g.mobs[victim].mode != 0 {
		t.Fatal("victim not removed")
	}
}
