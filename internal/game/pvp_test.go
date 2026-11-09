package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// pvpSpot finds a walkable PvP cell outside villages, arenas and war zones
// with a free neighbour.
func pvpSpot(t *testing.T, g *Game) (int, int) {
	t.Helper()
	for y := 1500; y < 3000; y += 7 {
		for x := 1500; x < 3000; x += 7 {
			a := g.data.attribute(x, y)
			if a&attrPvP == 0 || a&attrVillage != 0 || g.data.attribute(x+1, y)&attrPvP == 0 {
				continue
			}
			if arena(x, y) != 5 || g.data.village(x, y) != 5 || g.data.warArea(x, y) != -1 {
				continue
			}
			if g.data.Blocked(x, y) || g.data.Blocked(x+1, y) || g.cell(x, y) != 0 || g.cell(x+1, y) != 0 {
				continue
			}
			return x, y
		}
	}
	t.Skip("no PvP spot found")
	return 0, 0
}

// duel puts player 1 (attacker) and player 2 (victim) side by side.
func duel(t *testing.T, g *Game, x, y int) (*User, *wire.Mob, *wire.Mob) {
	u := placePlayer(g, x, y, 0)
	a := &g.mobs[1].info.Player
	a.Status.Level, a.BStatus.Level = 100, 100
	a.Status.Attack, a.Status.DEX = 20000, 1000
	a.Status.MaxHP, a.Status.CurHP = 100000, 100000
	u.attackRange = 2
	v := &g.mobs[2]
	g.users[2].status = userPlay
	v.mode = 1
	vp := &v.info.Player
	vp.Status.Level, vp.BStatus.Level = 100, 100
	vp.Equip[0].Effect[1].Value = evoMortal
	vp.Inventory[63].Effect[0].Index = 75
	vp.BStatus.CON = 2000 // a sturdy victim
	v.target = wire.Position{X: uint16(x + 1), Y: uint16(y)}
	g.setCell(x+1, y, 2)
	g.getCurrentScore(2)
	vp.Status.DEX = 0
	vp.Status.CurHP = vp.Status.MaxHP
	return u, a, vp
}

func hit(g *Game, u *User, target int, skill int16, dmg int64) {
	u.lastAttack = u.lastAttack.AddDate(-1, 0, 0)
	p := wire.Attack{Header: wire.NewHeader(wire.OpCliAttackOne, uint16(u.id)), SkillID: skill}
	p.Header.Size = attackSize1
	p.Target[0] = wire.AttackTarget{Index: int32(target), Damage: dmg}
	g.requestAttack(u, packetRaw(wire.MustMarshal(&p)[:attackSize1]))
}

func TestPvPHitAndGuilty(t *testing.T) {
	g := worldGame(t)
	x, y := pvpSpot(t, g)
	u, a, vp := duel(t, g, x, y)
	hit(g, u, 2, -1, -2)
	if vp.Status.CurHP >= vp.Status.MaxHP {
		t.Fatalf("no damage at %dx%d", x, y)
	}
	if g.guilty(1) != 8 {
		t.Fatalf("attacker guilty %d, want 8", g.guilty(1))
	}
	// The guilty mark wears off with the regen ticks.
	for range 16 * 8 {
		g.secCounter++
		g.regenTimer()
	}
	if g.guilty(1) != 0 {
		t.Fatalf("guilty still %d", g.guilty(1))
	}
	_ = a
}

func TestNoPvPInTown(t *testing.T) {
	g := worldGame(t)
	c := g.data.Cities[0]
	x, y := int(c.CityX)+3, int(c.CityY)+3 // inside the first city
	if g.data.attribute(x, y)&attrVillage == 0 {
		t.Skipf("%dx%d is not a village", x, y)
	}
	u, _, vp := duel(t, g, x, y)
	hit(g, u, 2, -1, -2)
	if vp.Status.CurHP != vp.Status.MaxHP {
		t.Fatalf("damage in a village: %d/%d", vp.Status.CurHP, vp.Status.MaxHP)
	}
}

func TestPvPKillPenalties(t *testing.T) {
	g := worldGame(t)
	x, y := pvpSpot(t, g)
	u, a, vp := duel(t, g, x, y)
	a.Inventory[63].Effect[0].Index = 75
	vp.Exp = nextLevel[evoMortal][100] + 1000000
	exp := vp.Exp
	vp.Status.CurHP = 1
	hit(g, u, 2, -1, -2)
	if vp.Status.CurHP != 0 {
		t.Fatal("victim did not die")
	}
	if pkPoint(a) != 72 {
		t.Fatalf("killer PK %d, want 72 (CP -3)", pkPoint(a))
	}
	if curKill(a) != 1 || totKill(a) != 1 {
		t.Fatalf("frags %d/%d", curKill(a), totKill(a))
	}
	if vp.Exp >= exp {
		t.Fatal("victim lost no exp")
	}

	// Killing a guilty player costs no CP.
	g.requestResurrect(g.users[2])
	g.setCell(int(g.mobs[2].target.X), int(g.mobs[2].target.Y), 0)
	g.mobs[2].target = wire.Position{X: uint16(x + 1), Y: uint16(y)}
	g.setCell(x+1, y, 2)
	vp.Status.CurHP = 1
	setGuilty(vp, 8)
	hit(g, u, 2, -1, -2)
	if vp.Status.CurHP != 0 || pkPoint(a) != 72 {
		t.Fatalf("guilty victim: hp %d, killer PK %d", vp.Status.CurHP, pkPoint(a))
	}
}

func TestPKModeBlocksTrade(t *testing.T) {
	g, a, b := tradeGame(t)
	g.requestPKMode(a, packet(&wire.SignalValue{Header: wire.NewHeader(OpCliPKMode, 1), Value: 1}))
	tr := emptyTrade()
	tr.ClientID = 2
	g.requestTrade(a, packet(&tr))
	if a.trade.ClientID != 0 {
		t.Fatal("trade opened in PK mode")
	}
	_ = b
}

func TestPvPSkillDamage(t *testing.T) {
	g := worldGame(t)
	skill := findSkill(t, g, func(id int, s *SkillData) bool {
		return s.InstanceType >= 2 && s.InstanceType <= 5 && s.Range >= 2 && s.Maxtarget >= 1 && id/24 == 1
	})
	x, y := pvpSpot(t, g)
	u, a, vp := duel(t, g, x, y)
	a.ClassInfo = 1
	a.Learn[0] = 1 << uint(skill%24)
	a.Status.Mastery = [4]uint16{200, 200, 200, 200}
	a.Status.INT = 2000
	a.Status.MaxMP, a.Status.CurMP = 50000, 50000
	hit(g, u, 2, int16(skill), -1)
	if vp.Status.CurHP >= vp.Status.MaxHP {
		t.Fatalf("skill %d did no PvP damage", skill)
	}
}
