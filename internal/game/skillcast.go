package game

import (
	"time"

	"dbsrv/internal/wire"
)

// Skills (RequestAttack.cpp damage = -1 branch, GetSkillDamage_PvM,
// GetSkillDamage_2, GetManaSpent, SetAffect, SetTick, ProcessAffect,
// SendAffect).

const (
	OpSrvAffect = 0x3B9
)

// skillCast holds the per-packet values RequestAttack derives once.
type skillCast struct {
	skill     int
	mastery   int // userMastery: Status.Mastery[skill kind]
	delay     int // 100 + mastery: buff duration factor
	maxTarget int // SkillData.Maxtarget
}

// checkSkill validates a skill use (class, learned, cooldown, gold, mana)
// and spends the mana. ok = false means drop the whole packet.
func (g *Game) checkSkill(u *User, p *wire.Attack) (sc skillCast, ok bool) {
	pl := &g.mobs[u.id].info.Player
	skill := int(p.SkillID)
	sc = skillCast{skill: skill, delay: 100, maxTarget: 1}
	if skill < 0 || skill >= len(g.data.Skills) {
		return sc, true // plain attack
	}
	sd := &g.data.Skills[skill]
	kind := 0
	switch {
	case skill >= 0 && skill < 96:
		kind = (skill%24)>>3 + 1
		if skill/24 != int(pl.ClassInfo) {
			return sc, false
		}
		// The C++ computed the learned bit but never checked it; a skill the
		// character has not learned is refused here.
		if pl.Learn[0]&(1<<uint(skill%24)) == 0 {
			return sc, false
		}
	case skill >= 97 && skill <= 102:
		if pl.Learn[0]&(1<<uint(24+skill-96)) == 0 {
			return sc, false
		}
	case skill >= 110 && skill <= 127, skill >= 200 && skill <= 248:
		return sc, false
	}
	if skill > 0 && skill < 97 {
		delay := time.Duration(sd.Delay) * time.Second
		if pl.AffectInfo&affSkillDelay != 0 {
			delay -= time.Second
		}
		now := time.Now()
		if now.Sub(u.skillUsed[skill]) < delay-500*time.Millisecond {
			return sc, false
		}
		u.skillUsed[skill] = now
	}
	sc.mastery = int(pl.Status.Mastery[kind])
	sc.delay = 100 + sc.mastery
	sc.maxTarget = sd.Maxtarget
	if skill == 85 { // golden shield costs gold
		cost := int32(sc.mastery * 100)
		if pl.Gold < cost {
			return sc, false
		}
		pl.Gold -= cost
		g.sendEtc(u)
	}
	if skill < 98 {
		mana := manaSpent(sd.Mana, int(pl.SaveMana), sc.mastery)
		if skill == 31 {
			mana = int(pl.Status.CurMP)
		}
		if int(pl.Status.CurMP)-mana < 0 {
			g.sendSetHpMp(u)
			return sc, false
		}
		pl.Status.CurMP -= int32(mana)
	}
	if pl.ClassInfo == 3 {
		g.removeAffect(u.id, 28) // DoRemoveHide
	}
	g.removeAffect(u.id, 24) // DoRemovePossuido
	return sc, true
}

// manaSpent is GetManaSpent.
func manaSpent(base, saveMana, mastery int) int {
	m := ((mastery >> 1) + 100) * base / 100
	return (100 - saveMana) * m / 100
}

// removeAffect clears an affect index from a player and resends the score.
func (g *Game) removeAffect(id, index int) {
	if id <= 0 || id >= MaxPlayer {
		return
	}
	for i := range g.mobs[id].info.Affects {
		if int(g.mobs[id].info.Affects[i].Index) == index {
			g.mobs[id].info.Affects[i] = wire.Affect{}
			g.getCurrentScore(id)
			g.sendScore(id)
		}
	}
}

// skillDamagePvM is GetSkillDamage_PvM.
func (g *Game) skillDamagePvM(skill, id, weather int) int {
	m := &g.mobs[id]
	mob := &m.info.Player
	sd := &g.data.Skills[skill]
	typ := sd.InstanceType
	kind := (skill%24)>>3 + 1
	level := int(mob.Status.Level)
	if evolution(mob) >= evoCelestial {
		level += 400
	}
	level = min(max(level, 0), 600)
	mastery := int(mob.Status.Mastery[kind])
	base, affectV := sd.InstanceValue, sd.AffectValue
	weapon := m.combat.weaponDamage
	intel, learn := int(mob.Status.INT), mob.Learn[0]
	ret := 0
	switch {
	case typ == 0:
		switch skill {
		case 11:
			ret = mastery/10 + affectV
		case 13:
			ret = mastery*3>>2 + affectV
		case 41:
			ret = mastery/25 + 2
		case 43:
			ret = mastery/3 + affectV
		case 44:
			ret = (mastery*3/20 + affectV) << 1
		case 45:
			ret = mastery/10 + affectV
		}
	case typ >= 1 && typ <= 5:
		group := skill >> 3
		switch {
		case skill == 97:
			ret = level*15 + base
		case mob.ClassInfo == 0 && group == 1:
			ret = base + mastery + level>>1 + int(mob.Status.DEX) + weapon*3
		case mob.ClassInfo == 0:
			ret = base + mastery + level>>1 + weapon + intel/2
			if skill == 7 || (skill >= 17 && skill <= 23 && learn&0x800000 != 0) {
				ret = base + mastery + level>>1 + weapon + intel/2*108/100
			}
			if skill == 7 {
				ret += ret * 10 / 100
			}
		case mob.ClassInfo == 1:
			ret = base + mastery + level>>1 + intel/2 + weapon
			if (skill >= 32 && skill <= 39 && learn&0x8000 != 0) || (skill == 40 && learn&0x800000 != 0) {
				ret = base + mastery + level>>1 + weapon + intel/2*115/100
			}
			if skill == 28 {
				ret += int(mob.Status.CurHP) * 10 / 100
			}
		case mob.ClassInfo == 2:
			ret = base + mastery + level>>1 + intel/2
			if skill == 48 || skill == 50 || skill == 52 || (skill == 55 && learn&0x80 != 0) {
				ret = base + mastery + level>>1 + weapon + intel/2*115/100
			}
			ret += ret * 8 / 100
		case mob.ClassInfo == 3:
			ret = base + mastery + level>>1 + int(mob.Status.STR)*3 + weapon*3
		}
		switch {
		case weather == 1 && (skill == 39 || skill == 48 || skill == 17):
			ret = ret * 90 / 100
		case weather == 1 && skill == 33:
			ret = ret * 150 / 100
		case weather == 2 && typ == 3:
			ret = ret * 120 / 100
		}
		switch {
		case skill == 97:
			return ret
		case mob.ClassInfo != 0 || group != 1:
			if mob.ClassInfo == 3 {
				ret = ret * 5 / 4
			} else {
				ret = (m.combat.magicIncrement*2 + 100) * ret / 100
				ret = ret * 5 / 4
			}
		default:
			return ret * 5 / 4
		}
	case typ == 6:
		ret = mastery*3 + base
	case typ == 11:
		return base
	default:
		ret = m.combat.magicIncrement
	}
	if skill == 79 {
		ret = int(mob.Status.Attack)
	}
	return ret
}

// skillDamage2 is GetSkillDamage_2: skill damage against defense.
func skillDamage2(damage, ac, master int) int {
	calc := damage - ac>>1
	master = min(master, 15)
	calc = calc * (rnd()%(21-master) + master + 90) / 100
	switch {
	case calc < -50:
		calc = 0
	case calc <= 0:
		calc = (calc + 50) / 10
	case calc <= 45:
		calc = calc*5>>2 + 5
	}
	return calc
}

// skillOnTarget is the damage = -1 branch of RequestAttack for one target.
// It returns the damage to go through the common hit path (<= 0: none) and
// false to skip the target.
// TODO(phase C): PvP skill damage, summon player (9), knockback (6), Exterminar (22) movement, 79/30/41/98/102 specials, the
// 22/23 auras.
func (g *Game) skillOnTarget(u *User, p *wire.Attack, i, tid int, sc *skillCast, master int, sameSide bool) (int, bool) {
	me := &g.mobs[u.id]
	mob := &g.mobs[tid]
	skill := sc.skill
	if skill < 0 || skill >= len(g.data.Skills) {
		return 0, false
	}
	sd := &g.data.Skills[skill]
	if i >= sc.maxTarget {
		return 0, false
	}
	if sd.PartyCheck != 0 && !sameSide {
		return 0, false
	}
	rng := sd.Range + 3
	if me.info.Player.Learn[0]&0x20000000 != 0 {
		rng++
	}
	if d := distance(int(me.target.X), int(me.target.Y), int(mob.target.X), int(mob.target.Y)); d > rng && skill != 42 {
		if distance(int(me.target.X), int(me.target.Y), int(mob.last.X), int(mob.last.Y)) > rng {
			return 0, false
		}
	}
	dam := 0
	cancel := false
	switch typ := sd.InstanceType; {
	case typ >= 1 && typ <= 5:
		pvp := tid < MaxPlayer
		def := int(mob.info.Player.Status.Defense)
		if pvp {
			dam = g.skillDamagePvP(skill, u.id, 0)
			def *= 3
		} else {
			dam = g.skillDamagePvM(skill, u.id, 0)
		}
		me.combat.attackCount = (me.combat.attackCount + 1) & 0x3FF
		dam = skillDamage2(dam, def, master)
		if typ == 1 {
			res := 10 // mobs resist holy damage by 10
			if pvp {
				res = int(mob.info.Player.Resist[2]) // players: holy resistance / 4
			}
			if ig := me.combat.ignoreResistance; ig > 0 && res > 0 {
				res -= res * ig / 100
			}
			if pvp {
				res /= 4
			}
			dam = (maxResist - res) * dam / 100
		} else {
			res := int(mob.info.Player.Resist[typ-2])
			if ig := me.combat.ignoreResistance; ig > 0 && res > 0 {
				res -= res * ig / 100
			}
			dam = (maxResist - res) * dam / 100
		}
		if skill == 79 {
			if pvp {
				dam = getDamage(g.skillDamagePvP(skill, u.id, 0), int(mob.info.Player.Status.Defense)*2, master)
			} else {
				dam = getDamage(g.skillDamagePvM(skill, u.id, 0), int(mob.info.Player.Status.Defense), master)
			}
		}
		switch typ {
		case 2:
			f := 10 - i
			if f < 0 {
				f = 0
				p.Target[i].Index = 0
			}
			dam = dam * f / 10
		case 4:
			f := 10 - i<<1
			if f < 0 {
				f = 0
				p.Target[i].Index = 0
			}
			dam = dam * f / 10
		}
	case typ == 6: // heal
		if mob.info.Player.CapeInfo == 4 {
			return 0, false
		}
		if skill == 27 {
			dam = sd.InstanceValue + sc.mastery*2
		} else {
			dam = sc.mastery*3>>1 + sd.InstanceValue
		}
		dam = -dam
		if dam < 0 && dam > -6 {
			dam = -6
		}
		pl := &me.info.Player
		if evolution(pl) >= evoCelestial {
			dam -= 150
			if learned(pl, 31) {
				dam -= 650
			}
		} else if ev := evolution(pl); ev == evoArch || ev == evoMortal {
			dam -= 100
			if learned(pl, 31) {
				dam -= 100
			}
		}
		if tid < MaxPlayer && g.guilty(tid) != 0 { // healing a guilty player makes you guilty
			g.markGuiltyAnywhere(u.id)
		}
		g.applyHP(tid, dam)
	case typ == 7: // calm a mob
		if tid >= MaxPlayer {
			mob.mode = modeNPCIdle
			mob.combat.currentTarget = 0
			mob.combat.enemies = [4]int{}
		}
	case typ == 8: // cleanse debuffs
		changed := false
		for k := range mob.info.Affects {
			a := int(mob.info.Affects[k].Index)
			if a == 1 || a == 3 || a == 5 || a == 7 || a == 10 || a == 12 || a == 20 || a == 56 ||
				(me.info.Player.Learn[0]&0x80 != 0 && a == 32) {
				mob.info.Affects[k] = wire.Affect{}
				changed = true
			}
		}
		if changed {
			g.getCurrentScore(tid)
			g.sendScore(tid)
		}
	case typ == 9: // summon a player: ask the target to come ("/relo")
		g.summonSkill(u, tid, sc.mastery)
		return 0, false
	case typ == 10: // taunt: mobs fighting the target turn on the caster
		for j := MaxPlayer; j < MaxMob; j++ {
			o := &g.mobs[j]
			if o.mode != modeBattle || o.combat.currentTarget != tid {
				continue
			}
			for k, e := range o.combat.enemies {
				if e == tid {
					o.combat.enemies[k] = u.id
				}
			}
			o.combat.currentTarget = u.id
		}
	case typ == 11: // evocations (BeastMaster 56..63)
		if i == 0 {
			g.castEvocation(u, skill)
		}
		return 0, false
	case typ == 12: // ethereal flames: dispel one strong buff
		if me.info.Player.ClassInfo != 2 {
			break
		}
		r1, r2 := rnd()%200, rnd()%200
		rate := 15
		if me.info.Player.Learn[0]&(1<<7) != 0 {
			rate += 10
		}
		if r1 >= r2 && r2 <= r1+rate {
			for k, a := range mob.info.Affects {
				if a.Index == 18 || a.Index == 16 || a.Index == 19 || a.Index == 24 {
					mob.info.Affects[k] = wire.Affect{}
					g.sendScore(tid)
					break
				}
			}
		}
	}
	if skill == 22 { // Exterminar: all mana into the hit
		pl := &me.info.Player
		mp := float64(pl.Status.CurMP)
		pl.Status.CurMP = 0
		p.CurrentMP = 0
		dam = int((float64(dam) + mp*1.1 + float64(pl.Status.INT)*1.1) * 1.05)
	}
	if skill == 47 && rnd()%100 < 25 { // cancel: 25% miss
		p.Target[i].Damage = -3
		return 0, false
	}
	if skill == 47 && mob.info.Player.ClassInfo == 3 {
		for k, a := range mob.info.Affects {
			if a.Index == 19 {
				mob.info.Affects[k] = wire.Affect{}
				g.getCurrentScore(tid)
				cancel = true
				break
			}
		}
	}

	// Buffs and debuffs.
	apply := true
	if sd.Aggressive != 0 {
		if sameSide {
			apply = false
		}
		if r := sd.AffectResist; r >= 1 && r <= 3 && rnd()%100 < int(mob.info.Player.Resist[r]) {
			apply = false
		}
		if mob.info.AffectInfo&2 != 0 || mob.info.Player.CapeInfo == 6 {
			apply = false
		}
	}
	if apply && !cancel {
		a := g.setAffect(tid, skill, sc.delay, sc.mastery)
		t := g.setTick(tid, skill, sc.delay, sc.mastery)
		if a || t {
			switch skill {
			case 64, 66, 68, 70, 71:
				if tu := g.player(tid); tu != nil {
					g.sendEquip(tu)
				}
			}
			g.getCurrentScore(tid)
			g.sendScore(tid)
		}
	}
	if cancel {
		g.getCurrentScore(tid)
		g.sendScore(tid)
	}
	return dam, true
}

// applyHP subtracts dam (negative = heal) from a mob's HP, through HP items.
func (g *Game) applyHP(id, dam int) {
	s := &g.mobs[id].info.Player.Status
	if hp := g.mobs[id].info.Player.Equip[13].Index; hp == 786 || hp == 1936 || hp == 1937 {
		mult := max(itemSanc(&g.mobs[id].info.Player.Equip[13]), 2)
		switch hp {
		case 1936:
			mult *= 10
		case 1937:
			mult *= 1000
		}
		s.CurHP -= int32(dam / mult)
	} else {
		s.CurHP -= int32(dam)
	}
	s.CurHP = min(s.CurHP, s.MaxHP)
}

// emptyAffect is GetEmptyAffect: the slot already holding index, else the
// first free one.
func (g *Game) emptyAffect(id, index int) int {
	aff := &g.mobs[id].info.Affects
	for i := range aff {
		if int(aff[i].Index) == index {
			return i
		}
	}
	for i := range aff {
		if aff[i].Index == 0 {
			return i
		}
	}
	return -1
}

// setAffect is SetAffect(clientId, skill, delay, level): players only.
func (g *Game) setAffect(id, skill, delay, level int) bool {
	if id < 0 || id >= MaxPlayer {
		return false
	}
	m := &g.mobs[id]
	sd := &g.data.Skills[skill]
	typ := sd.AffectType
	if sd.Aggressive != 0 && m.info.Player.AffectInfo&affResist != 0 {
		return false
	}
	slot := g.emptyAffect(id, typ)
	if typ <= 0 || slot < 0 || slot >= 24 {
		return false
	}
	a := &m.info.Affects[slot]
	a.Index = uint8(typ)
	a.Master = uint8(sd.AffectValue)
	a.Time = uint32(sd.Time * delay / 100)
	if level > int(a.Value) {
		a.Value = uint16(level)
	}
	g.getCurrentScore(id)
	return true
}

// setTick is SetTick: the skill's tick affect (debuffs on mobs too).
func (g *Game) setTick(id, skill, delay, level int) bool {
	m := &g.mobs[id]
	if id > MaxPlayer && m.info.Player.Info&0x3F == 1 {
		return false
	}
	sd := &g.data.Skills[skill]
	typ := sd.TickType
	if m.info.AffectInfo&2 != 0 && sd.Aggressive != 0 {
		return false
	}
	slot := g.emptyAffect(id, typ)
	if typ <= 0 || slot < 0 {
		return false
	}
	a := &m.info.Affects[slot]
	a.Index = uint8(typ)
	a.Master = uint8(sd.TickValue)
	a.Time = uint32(delay * sd.Time / 100)
	if delay >= 10000 {
		a.Time = 10000
	}
	a.Value = uint16(level)
	g.getCurrentScore(id)
	return true
}

// sendAffect is SendAffect (0x3B9): the affect list, divine (34) first.
// Date buffs (Divina/Sephira/Saúde/Revigorante) are refreshed and their
// time shown in days left, as the C++ did.
func (g *Game) sendAffect(u *User) {
	if u == nil || u.status != userPlay {
		return
	}
	aff := &g.mobs[u.id].info.Affects
	for i := range aff {
		if aff[i].Index == 34 {
			aff[0], aff[i] = aff[i], aff[0]
			break
		}
	}
	left := g.refreshDateBuffs(u)
	out := wire.AffectList{Header: wire.NewHeader(OpSrvAffect, uint16(u.id))}
	for i, a := range aff {
		if a.Index == 0 || a.Time == 0 {
			continue
		}
		out.Affect[i] = a
		for _, b := range dateBuffs {
			if a.Index == b.idx && b.match(&aff[i]) {
				out.Affect[i].Time = uint32(left[a.Index])
			}
		}
	}
	g.queue(u, &out)
}

// processAffect is ProcessAffect: tick the affects of a mob or player.
// TODO(phase C): auras 22/23 (automatic area attacks), familiar auto-attack.
func (g *Game) processAffect(id int) {
	m := &g.mobs[id]
	pl := &m.info.Player
	if id < MaxPlayer && g.users[id].status == userPlay {
		g.refreshDateBuffs(g.users[id]) // keep them at 999 until their date
	}
	expired, unTransform, hpChanged := false, false, false
	delta := int32(0)
	for i := range m.info.Affects {
		a := &m.info.Affects[i]
		if a.Index == 0 {
			continue
		}
		maxHP, curHP := pl.Status.MaxHP, pl.Status.CurHP
		switch a.Index {
		case 17: // aura of life
			hp := min(max(curHP+int32(a.Value)<<1+int32(a.Master)+50, 1), maxHP)
			delta = hp - curHP
			pl.Status.CurHP = hp
			hpChanged = true
		case 20: // poison: players lose 10% (at most 1000)
			if id < MaxPlayer {
				hp := int32(float32(curHP) * 0.9)
				hp = max(hp, curHP-1000)
				hp = min(max(hp, 1), maxHP)
				delta = hp - curHP
				pl.Status.CurHP = hp
				hpChanged = true
			}
		}
		a.Time--
		if int32(a.Time) <= 0 {
			if a.Index == 16 {
				unTransform = true
				pl.Equip[0].Index = uint16(pl.Equip[0].Effect[1].Index)
			}
			*a = wire.Affect{}
			expired = true
		}
	}
	if hpChanged {
		g.multicast2(int(m.target.X), int(m.target.Y), &wire.HpChange{
			Header: wire.NewHeader(wire.OpSrvHpChange, uint16(id)), CurHP: pl.Status.CurHP, Incress: int16(delta)}, 0)
	}
	if expired {
		g.getCurrentScore(id)
		g.sendScore(id)
		if u := g.player(id); unTransform && u != nil {
			g.sendEquip(u)
		}
	}
}

// resurrectSkill handles skill 99 (self resurrection, the only skill a
// dead player may use): 50% back with 10% HP in place, else 2 HP in town.
func (g *Game) resurrectSkill(u *User, raw []byte) {
	m := &g.mobs[u.id]
	g.multicastRaw(int(m.target.X), int(m.target.Y), raw)
	if rnd()%100 <= 50 {
		m.info.Player.Status.CurHP = m.info.Player.Status.MaxHP / 10
		u.potionHP = m.info.Player.Status.CurHP
		p := g.createMobPacket(u.id)
		g.multicast2(int(m.target.X), int(m.target.Y), &p, 0)
	} else {
		m.info.Player.Status.CurHP = 2
		u.potionHP = 2
		g.doRecall(u.id)
	}
	g.sendSetHpMp(u)
	g.sendScore(u.id)
}
