package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func TestRouteWalksTowardTarget(t *testing.T) {
	g := worldGame(t)
	// Find two walkable points 5 cells apart on flat ground.
	for y := 2000; y < 2200; y++ {
		x := 2100
		tx, ty := x+5, y
		if g.data.Blocked(x, y) || g.data.Blocked(tx, ty) {
			continue
		}
		var r [24]byte
		g.data.getRoute(x, y, &tx, &ty, &r)
		if routeLen(&r) == 0 {
			continue
		}
		if tx <= x || r[0] != '6' {
			t.Fatalf("route %q to %d,%d", r[:routeLen(&r)], tx, ty)
		}
		return
	}
	t.Skip("no flat ground found")
}

func TestIdleMobsWalk(t *testing.T) {
	g := worldGame(t)
	start := map[int]wire.Position{}
	for id := firstNPC; id < MaxMob; id++ {
		if g.mobs[id].mode == modeNPCIdle {
			start[id] = g.mobs[id].target
		}
	}
	for range 60 {
		g.secCounter++
		g.mobTimer()
	}
	moved := 0
	for id, p := range start {
		m := &g.mobs[id]
		if m.mode != 0 && m.target != p {
			moved++
			if g.cell(int(m.target.X), int(m.target.Y)) != uint16(id) {
				t.Fatalf("mob %d moved but grid not updated", id)
			}
		}
	}
	if moved == 0 {
		t.Fatal("no idle mob moved in 60 ticks")
	}
	t.Logf("%d of %d idle mobs moved", moved, len(start))
}

// hostileMob finds a monster that attacks players (clan table) outside towns.
func hostileMob(t *testing.T, g *Game) int {
	t.Helper()
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		pl := &m.info.Player
		cape := int(pl.CapeInfo)
		if m.mode != modeNPCIdle || cape < 0 || cape >= 9 || clanTable[cape][0] > 0 || pl.Info&0x3F != 0 {
			continue
		}
		if pl.Status.Attack <= 0 || pl.BStatus.INT < 50 || m.npc.leader != 0 {
			continue
		}
		if g.data.attribute(int(m.target.X), int(m.target.Y))&attrVillage != 0 {
			continue
		}
		return id
	}
	t.Fatal("no hostile mob")
	return 0
}

func TestMobAggroAttacksAndKills(t *testing.T) {
	g := worldGame(t)
	id := hostileMob(t, g)
	mt := g.mobs[id].target
	u := placePlayer(g, int(mt.X)+1, int(mt.Y)+1, 0)
	pl := &g.mobs[1].info.Player
	pl.Status.MaxHP, pl.Status.CurHP = 1000000, 1000000
	pl.Status.Defense = 0

	hit := false
	for range 400 {
		g.secCounter++
		g.mobTimer()
		if pl.Status.CurHP < 1000000 {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("mob %d (mode %d, target %d) never hit the player", id, g.mobs[id].mode, g.mobs[id].combat.currentTarget)
	}
	if g.mobs[id].mode != modeBattle {
		t.Fatalf("attacking mob in mode %d", g.mobs[id].mode)
	}

	// Now let it kill the player.
	pl.Status.CurHP = 1
	for range 400 {
		g.secCounter++
		g.mobTimer()
		if pl.Status.CurHP == 0 {
			break
		}
	}
	if pl.Status.CurHP != 0 {
		t.Fatal("player never died")
	}
	for range 40 {
		g.secCounter++
		g.mobTimer()
	}
	if g.mobs[id].combat.currentTarget == 1 {
		t.Fatal("mob still targets the dead player")
	}

	g.requestResurrect(u)
	if pl.Status.CurHP != 2 {
		t.Fatalf("resurrected with %d hp", pl.Status.CurHP)
	}
	if p := g.mobs[1].target; distance(int(p.X), int(p.Y), int(mt.X), int(mt.Y)) < 20 {
		t.Fatalf("resurrected next to the mob at %v", p)
	}
}

func TestRegen(t *testing.T) {
	g := worldGame(t)
	placePlayer(g, 2112, 2042, 0)
	s := &g.mobs[1].info.Player.Status
	s.MaxHP, s.CurHP, s.MaxMP, s.CurMP = 5000, 100, 5000, 100
	for range 16 {
		g.secCounter++
		g.regenTimer()
	}
	if s.CurHP <= 100 || s.CurMP <= 100 {
		t.Fatalf("no regen: hp %d mp %d", s.CurHP, s.CurMP)
	}
}

func TestEnemyInLineIsSeen(t *testing.T) {
	g := worldGame(t)
	id := hostileMob(t, g)
	mt := g.mobs[id].target
	// Same row, two cells to the right: the C++ scan missed this.
	x, y := int(mt.X)+2, int(mt.Y)
	if g.cell(x, y) != 0 || g.data.Blocked(x, y) {
		t.Skip("cell next to the mob is taken")
	}
	placePlayer(g, x, y, 0)
	if g.mobs[1].target.X != uint16(x) || g.mobs[1].target.Y != uint16(y) {
		t.Skip("player not placed in line")
	}
	if got := g.enemyFromView(id); got != 1 {
		t.Fatalf("enemyFromView = %d, want the player in line (1)", got)
	}
}
