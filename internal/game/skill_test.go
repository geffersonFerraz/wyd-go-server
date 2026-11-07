package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

// findSkill returns the first class skill (0..95) matching ok.
func findSkill(t *testing.T, g *Game, ok func(id int, s *SkillData) bool) int {
	t.Helper()
	for id := 0; id < 96; id++ {
		if s := &g.data.Skills[id]; s.PassiveCheck == 0 && ok(id, s) {
			return id
		}
	}
	t.Skip("no matching skill in skilldata.csv")
	return -1
}

// caster makes slot 1 a high-level player of the skill's class who learned it.
func caster(g *Game, x, y, skill int) (*User, *wire.Mob) {
	u := placePlayer(g, x, y, 0)
	pl := &g.mobs[1].info.Player
	pl.ClassInfo = uint8(skill / 24)
	pl.Learn[0] = 1 << uint(skill%24)
	pl.Status.Level = 200
	pl.Status.Mastery = [4]uint16{200, 200, 200, 200}
	pl.Status.MaxMP, pl.Status.CurMP = 50000, 50000
	pl.Status.INT = 2000
	return u, pl
}

func castAt(g *Game, u *User, skill, target int) {
	u.lastAttack = time.Time{}
	p := wire.Attack{Header: wire.NewHeader(wire.OpCliAttackOne, uint16(u.id)), SkillID: int16(skill)}
	p.Header.Size = attackSize1
	p.Target[0] = wire.AttackTarget{Index: int32(target), Damage: -1}
	g.requestAttack(u, packetRaw(wire.MustMarshal(&p)[:attackSize1]))
}

func TestDamageSkillHitsMob(t *testing.T) {
	g := worldGame(t)
	skill := findSkill(t, g, func(id int, s *SkillData) bool {
		return s.InstanceType >= 2 && s.InstanceType <= 5 && s.Range >= 2 && s.Maxtarget >= 1 && id/24 == 1
	})
	npc := 0
	for id := firstNPC; id < MaxMob && npc == 0; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && m.info.Player.Info&0x3F == 0 && m.info.Player.Equip[13].Index == 0 &&
			g.data.attribute(int(m.target.X), int(m.target.Y))&attrVillage == 0 {
			npc = id
		}
	}
	mt := g.mobs[npc].target
	u, pl := caster(g, int(mt.X)+1, int(mt.Y), skill)
	g.getCurrentScore(1)
	pl.Status.MaxMP, pl.Status.CurMP = 50000, 50000
	hp := g.mobs[npc].info.Player.Status.CurHP
	castAt(g, u, skill, npc)
	if pl.Status.CurMP >= 50000 {
		t.Fatal("no mana spent")
	}
	if g.mobs[npc].mode != 0 && g.mobs[npc].info.Player.Status.CurHP >= hp {
		t.Fatalf("skill %d did no damage (hp %d)", skill, g.mobs[npc].info.Player.Status.CurHP)
	}

	// Cooldown: casting again right away is refused (no mana spent).
	if g.data.Skills[skill].Delay > 1 {
		mp := pl.Status.CurMP
		castAt(g, u, skill, npc)
		if pl.Status.CurMP != mp {
			t.Fatal("cast again during the cooldown")
		}
	}
}

func TestUnlearnedSkillRefused(t *testing.T) {
	g := worldGame(t)
	skill := findSkill(t, g, func(id int, s *SkillData) bool { return s.Mana > 0 && id/24 == 1 })
	u, pl := caster(g, 2112, 2042, skill)
	pl.Learn[0] = 0
	castAt(g, u, skill, 1)
	if pl.Status.CurMP != 50000 {
		t.Fatal("unlearned skill was cast")
	}
}

func TestHealSkill(t *testing.T) {
	g := worldGame(t)
	skill := findSkill(t, g, func(id int, s *SkillData) bool { return s.InstanceType == 6 })
	u, pl := caster(g, 2112, 2042, skill)
	pl.BStatus.Level = 200
	g.getCurrentScore(1)
	pl.Status.CurMP = pl.Status.MaxMP
	pl.Status.CurHP = 100
	if pl.Status.MaxHP <= 200 {
		t.Fatalf("test setup: max hp %d", pl.Status.MaxHP)
	}
	castAt(g, u, skill, 1)
	if pl.Status.CurHP <= 100 {
		t.Fatalf("heal skill %d: hp %d", skill, pl.Status.CurHP)
	}
}

func TestBuffSkillAndExpiry(t *testing.T) {
	g := worldGame(t)
	skill := findSkill(t, g, func(id int, s *SkillData) bool {
		return s.Aggressive == 0 && s.AffectType > 0 && s.InstanceType == 0 && s.Time > 0
	})
	u, _ := caster(g, 2112, 2042, skill)
	castAt(g, u, skill, 1)
	aff := &g.mobs[1].info.Affects
	slot := -1
	for i := range aff {
		if int(aff[i].Index) == g.data.Skills[skill].AffectType {
			slot = i
		}
	}
	if slot == -1 {
		t.Fatalf("skill %d gave no affect %d", skill, g.data.Skills[skill].AffectType)
	}
	time := aff[slot].Time
	g.processAffect(1)
	if aff[slot].Time != time-1 && time > 1 {
		t.Fatalf("affect time %d -> %d", time, aff[slot].Time)
	}
	for range time + 1 {
		g.processAffect(1)
	}
	if aff[slot].Index != 0 {
		t.Fatal("affect did not expire")
	}
}

func TestSelfResurrectSkill(t *testing.T) {
	g := worldGame(t)
	u, pl := caster(g, 2112, 2042, 0)
	pl.Learn[0] = 1 << 27 // skill 99
	pl.Status.MaxHP, pl.Status.CurHP = 1000, 0
	castAt(g, u, 99, 1)
	if pl.Status.CurHP == 0 {
		t.Fatal("still dead after skill 99")
	}
}
