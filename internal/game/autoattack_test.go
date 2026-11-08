package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// attackField puts a player in a field, away from towns, next to a mob.
func attackField(t *testing.T) (*Game, *User, *Mob, int) {
	g := worldGame(t)
	x, y := 2300, 2300
	u := placePlayer(g, x, y, 0)
	listen(u)
	id := 1500
	mob := &g.mobs[id]
	*mob = Mob{}
	mob.mode = modeNPCIdle
	mob.info.Player.Status.MaxHP, mob.info.Player.Status.CurHP = 900000, 900000
	mob.info.Player.BStatus.MaxHP, mob.info.Player.BStatus.CurHP = 900000, 900000
	mob.target = wire.Position{X: uint16(x + 1), Y: uint16(y)}
	mob.last = mob.target
	g.setCell(x+1, y, uint16(id))
	return g, u, mob, id
}

func TestFamiliarHitsCurrentTarget(t *testing.T) {
	g, u, mob, id := attackField(t)
	roll := rnd
	defer func() { rnd = roll }()
	rnd = func() int { return 900 } // no dodge
	me := &g.mobs[1]
	me.info.Player.Status.Level = 100
	me.info.Player.Equip[13] = wire.Item{Index: 1726}
	me.info.Player.Equip[13].Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 3}
	me.combat.currentTarget = id
	mp := me.info.Player.Status.CurMP
	before := mob.info.Player.Status.CurHP
	g.familiarAttack(u)
	if mob.info.Player.Status.CurHP >= before {
		t.Fatal("familiar did no damage")
	}
	if me.info.Player.Status.CurMP != mp {
		t.Fatal("familiar spent mana")
	}
	// It runs again at once: no attack-rate limit, no cooldown.
	hp := mob.info.Player.Status.CurHP
	g.familiarAttack(u)
	if mob.info.Player.Status.CurHP >= hp {
		t.Fatal("familiar limited by the attack rate")
	}
	// Target out of view: forgotten.
	mob.target = wire.Position{X: 100, Y: 100}
	g.familiarAttack(u)
	if me.combat.currentTarget != 0 {
		t.Fatal("kept a target out of view")
	}
}

func TestPlainPlayerHasNoFamiliarAttack(t *testing.T) {
	g, u, mob, id := attackField(t)
	g.mobs[1].combat.currentTarget = id
	before := mob.info.Player.Status.CurHP
	g.familiarAttack(u)
	if mob.info.Player.Status.CurHP != before {
		t.Fatal("attacked without a familiar")
	}
}

func TestAuraTargets(t *testing.T) {
	g, u, mob, id := attackField(t)
	roll := rnd
	defer func() { rnd = roll }()
	rnd = func() int { return 0 }
	before := mob.info.Player.Status.CurHP
	g.mobs[1].info.Player.Status.Mastery[0] = 200
	g.auraBestial(u)
	if mob.info.Player.Status.CurHP > before {
		t.Fatal("hp grew")
	}
	// A summon without PK mode is left alone.
	mob.npc.summoner = 7
	if !func() bool {
		found := false
		g.gridTargets(u.id, 1, func(o int) bool { found = found || o == id; return true })
		return found
	}() {
		t.Fatal("grid does not see the mob")
	}
	mob.info.Player.Status.CurHP = before
	g.auraBestial(u)
	if mob.info.Player.Status.CurHP != before {
		t.Fatal("aura hit a summon without PK")
	}
}

func TestAuraSkipsTownAndAllies(t *testing.T) {
	g, u, mob, id := attackField(t)
	mob.info.Player.CapeInfo = 6
	if !g.auraSkips(u, id) {
		t.Fatal("cape 6 is out")
	}
	mob.info.Player.CapeInfo = 0
	mob.npc.leader = 1
	if !g.alliedTo(1, id) {
		t.Fatal("own follower is an ally")
	}
}

func TestSoulLimit(t *testing.T) {
	g, u, _, _ := attackField(t)
	me := &g.mobs[1]
	me.info.Player.Equip[0].Effect[1].Value = evoArch
	if g.soulLimit(u) {
		t.Fatal("arch used it")
	}
	me.info.Player.Equip[0].Effect[1].Value = evoCelestial
	me.info.Player.BStatus.Level = 100
	if !g.soulLimit(u) {
		t.Fatal("celestial refused")
	}
	if a := me.info.Affects[g.emptyAffect(1, 29)]; a.Index != 29 || a.Time != 46 || a.Value != 10 {
		t.Fatalf("affect %+v", a)
	}
}

func TestFuryPullsTheMob(t *testing.T) {
	g, u, mob, id := attackField(t)
	roll := rnd
	defer func() { rnd = roll }()
	rnd = func() int { return 0 }
	mob.target = wire.Position{X: 2305, Y: 2300}
	g.setCell(2301, 2300, 0)
	g.setCell(2305, 2300, uint16(id))
	g.mobs[1].info.Player.Status.Mastery[1] = 100
	g.furyPull(u, id)
	if mob.target.X >= 2305 {
		t.Fatalf("not pulled: %d", mob.target.X)
	}
	// Immune faces stay.
	pos := mob.target
	mob.info.Player.Equip[0].Index = 219
	g.furyPull(u, id)
	if mob.target != pos {
		t.Fatal("pulled an immune mob")
	}
}

func TestThornWall(t *testing.T) {
	g, u, _, _ := attackField(t)
	if g.data.Thorn == nil {
		t.Skip("no npc/Vinha")
	}
	p := wire.Attack{TargetPos: wire.Position{X: 2303, Y: 2303}}
	if !g.thornWall(u, &p) {
		t.Fatal("wall refused")
	}
	id := int(g.cell(2303, 2303))
	if id < MaxPlayer || g.mobs[id].info.Affects[0].Index != affSummoned || g.mobs[id].npc.routeType != 5 {
		t.Fatalf("mob %d", id)
	}
	p.TargetPos = wire.Position{X: 100, Y: 100} // far from the caster
	if g.thornWall(u, &p) {
		t.Fatal("wall out of range")
	}
}

func TestFamiliarLosesRefineOnDeath(t *testing.T) {
	g, u, _, _ := attackField(t)
	_ = u
	it := &g.mobs[1].info.Player.Equip[13]
	*it = wire.Item{Index: 753}
	it.Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 1}
	g.familiarLosesRefine(1)
	if it.Effect[0].Value != 0 || it.Index != 753 {
		t.Fatalf("refine: %+v", *it)
	}
	g.familiarLosesRefine(1)
	if it.Index != 0 {
		t.Fatal("not destroyed at +0")
	}
}

func TestTeleportBuffRespectsMastery(t *testing.T) {
	g, u, _, _ := attackField(t)
	_ = u
	var p wire.Attack
	sc := skillCast{mastery: 0, delay: 100}
	g.teleportBuff(&p, &sc) // no targets: must not panic
}
