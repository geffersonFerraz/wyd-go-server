package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Combat, slice 1: physical attacks on NPCs (RequestAttack.cpp, damage = -2),
// mob death and experience (MobKilled), level up (CMob::CheckGetLevel).
// Skills (damage = -1), PvP, drops and mob AI come in the next slices.

const (
	attackSize1 = 96  // sizeof p39D
	attackSize2 = 112 // sizeof p39D + sizeof st_Target
	attackSize  = 288 // sizeof p367

	magicTimestamp = 0x0E0A1ACA
	modeBattle     = 5
)

// getDamage is GetDamage.
func getDamage(dam, ac, combat int) int {
	calc := dam - ac>>1
	combat >>= 1
	if combat > 7 {
		combat = 7
	}
	inc := rnd()%(12-combat) + combat + 99
	calc = calc * inc / 100
	switch {
	case calc < -50:
		calc = 0
	case calc < 0:
		calc = (calc + 50) / 7
	case calc <= 50:
		calc = (calc*5)>>2 + 7
	}
	if calc <= 0 {
		calc = 1
	}
	return calc
}

// doubleCritical is GetDoubleCritical: bit 0 = double hit (attack speed vs
// the hit-rate table), bit 1 = critical.
func doubleCritical(c *combatState, critical uint8, clientCount *int16) uint8 {
	if *clientCount >= 1024 {
		*clientCount &= 0x3FF
	}
	if c.attackCount >= 1024 {
		c.attackCount &= 0x3FF
	}
	if *clientCount != c.attackCount {
		c.attackCount = *clientCount
	}
	var out uint8
	if hitRate[*clientCount&0x3FF] < ((c.attackSpeed>>4)-5)*100 {
		out |= 1
	}
	if rnd()%250 < int(critical) {
		out |= 2
	}
	c.attackCount++
	*clientCount++
	return out
}

// setBattle is SetBattle(mob, enemy): the mob turns hostile to enemy.
func (g *Game) setBattle(id, enemy int) {
	if id <= 0 || enemy <= 0 || id >= MaxMob || enemy >= MaxMob {
		return
	}
	m, e := &g.mobs[id], &g.mobs[enemy]
	if m.mode == 0 || e.mode == 0 {
		return
	}
	if u := g.player(id); u != nil && u.status != userPlay {
		return
	}
	if int(m.target.X) < int(e.target.X)-viewGrid || int(m.target.X) > int(e.target.X)+viewGrid ||
		int(m.target.Y) < int(e.target.Y)-viewGrid || int(m.target.Y) > int(e.target.Y)+viewGrid {
		return
	}
	if m.mode != modeBattle {
		g.kingdomHit(m.npc.generateID)
	}
	m.mode = modeBattle
	m.addEnemy(enemy)
	if m.npc.generateID == genKefra {
		g.forEachPlaying(func(o *User) {
			t := g.mobs[o.id].target
			if d := int(t.X) - int(m.target.X); d >= -30 && d <= 30 {
				if d := int(t.Y) - int(m.target.Y); d >= -30 && d <= 30 {
					m.addEnemy(o.id)
				}
			}
		})
	}
}

// addEnemy is CMob::AddEnemyList.
func (m *Mob) addEnemy(id int) {
	for _, e := range m.combat.enemies {
		if e == id {
			return
		}
	}
	for i, e := range m.combat.enemies {
		if e == 0 {
			m.combat.enemies[i] = id
			return
		}
	}
}

// multicastRaw is GridMulticast_2 for an already encoded packet.
func (g *Game) multicastRaw(x, y int, raw []byte) {
	minX, minY, maxX, maxY := viewRect(x, y)
	for ny := minY; ny < maxY; ny++ {
		for nx := minX; nx < maxX; nx++ {
			if u := g.player(int(g.cell(nx, ny))); u != nil && g.mobs[u.id].mode != 0 {
				g.queueRaw(u, raw)
			}
		}
	}
}

// manaShield is affect 18: 80% of the damage comes out of the MP, as long
// as at least 300 MP are left. It returns the remaining damage, whether the
// target has the shield, and whether it absorbed this hit.
func (g *Game) manaShield(tid, dam int) (left int, shielded, absorbed bool) {
	m := &g.mobs[tid]
	for i := range m.info.Affects {
		if m.info.Affects[i].Index != 18 {
			continue
		}
		part := dam * 80 / 100
		st := &m.info.Player.Status
		if int(st.CurMP)-part < 300 {
			return dam, true, false
		}
		st.CurMP -= int64(part)
		if u := g.player(tid); u != nil {
			u.potionMP = max(u.potionMP-int64(part), 0)
			g.setReqMp(u)
		}
		return dam - part, true, true
	}
	return dam, false, false
}

// sendHpMode is SendHpMode (0x292): the server's HP and user status, sent
// when the client acts on a state the server does not share (e.g. dead here
// but alive there).
func (g *Game) sendHpMode(u *User) {
	if u.status != userPlay {
		return
	}
	g.queue(u, &wire.HpMode{Header: wire.NewHeader(wire.OpSrvHpMode, uint16(u.id)),
		CurHP: g.mobs[u.id].info.Player.Status.CurHP, Status: int16(u.status)})
}

// sendSetHpMp is SendSetHpMp (0x181).
func (g *Game) sendSetHpMp(u *User) {
	if u.status != userPlay {
		return
	}
	g.setReqHp(u)
	g.setReqMp(u)
	s := &g.mobs[u.id].info.Player.Status
	// The C++ sends the pending potion values in the max fields.
	g.queue(u, &wire.SetHpMp{Header: wire.NewHeader(wire.OpSrvSetHpMp, uint16(u.id)),
		CurHP: s.CurHP, CurMP: s.CurMP, MaxHP: u.potionHP, MaxMP: u.potionMP})
}

func (g *Game) requestAttack(u *User, pkt protocol.Packet) {
	if u.status != userPlay {
		return
	}
	var p wire.Attack
	if wire.Unmarshal(pkt.Raw, &p) != nil || p.Header.TimeStamp == magicTimestamp {
		return
	}
	g.doAttack(u, p, false)
}

// doAttack is RequestAttack. auto marks the attacks the server makes for
// the player (familiar, auras), which skip the rate, cooldown, learned and
// mana checks.
func (g *Game) doAttack(u *User, p wire.Attack, auto bool) {
	me := &g.mobs[u.id]
	pl := &me.info.Player
	if pl.Status.CurHP == 0 && p.SkillID != 99 {
		g.sendHpMode(u)
		return
	}
	if !auto {
		now := time.Now()
		if now.Sub(u.lastAttack) < 900*time.Millisecond {
			return
		}
		u.lastAttack = now
	}
	var maxTarget int
	switch int(p.Header.Size) {
	case attackSize:
		maxTarget = 13
	case attackSize1:
		maxTarget = 1
	case attackSize2:
		maxTarget = 2
	default:
		return
	}
	skill := int(p.SkillID)
	if skill >= 0 && skill < 110 && g.data.Skills[skill].PassiveCheck == 1 {
		return
	}
	var sc skillCast
	if auto {
		sc = skillCast{skill: skill, auto: true, mastery: int(pl.Status.Mastery[0]), delay: 100 + int(pl.Status.Mastery[0]), maxTarget: 1}
		g.removeAffect(u.id, 24) // DoRemovePossuido
	} else {
		var ok bool
		if sc, ok = g.checkSkill(u, &p); !ok {
			return
		}
	}
	sc.startHP = int(pl.Status.CurHP)
	if skill == 99 {
		if pl.Status.CurHP == 0 {
			raw, _ := wire.Marshal(&p)
			g.resurrectSkill(u, raw[:p.Header.Size])
		}
		return
	}
	for i := 0; i < maxTarget; i++ {
		for j := i + 1; j < maxTarget; j++ {
			if p.Target[i].Index != 0 && p.Target[i].Index == p.Target[j].Index {
				p.Target[j].Index = 0
			}
		}
	}
	if skill == 79 { // Tempestade de Raios strikes the first target 6 times
		for i := 1; i < 6; i++ {
			p.Target[i] = p.Target[0]
		}
	}
	master := 0
	if pl.ClassInfo == 0 && me.npc.leader&16384 == 0 {
		master = min(max(int(pl.Status.Mastery[2])/20, 0), 15)
	}
	me.motion = 0
	var crit uint8

	for i := 0; i < maxTarget; i++ {
		t := &p.Target[i]
		tid := int(t.Index)
		if tid <= 0 || tid >= MaxMob {
			continue
		}
		mob := &g.mobs[tid]
		if mob.mode == 0 {
			g.sendRemoveMob(u, tid, 0)
			continue
		}
		if skill == 31 && t.Damage == -1 {
			if g.resurrectAlly(u, &p, i, tid, &sc) {
				return
			}
			continue
		}
		if mob.info.Player.Status.CurHP <= 0 {
			t.Index, t.Damage = 0, 0
			continue
		}
		if g.hitEventProgress(u, tid) {
			t.Index, t.Damage = 0, 0
			break
		}
		if g.warHitBlocked(u.id, tid) {
			t.Index, t.Damage = 0, 0
			continue
		}
		dam := int(t.Damage)
		orig := dam // -2 physical, -1 skill
		if dam != -2 && dam != -1 && dam != 0 {
			t.Damage = 0
			g.addCrack(u, 10, "attack with a forged damage")
			continue
		}
		leader, mobLeader, guild, mobGuild := g.sides(u.id, tid)
		sameSide := mobLeader == leader || mobGuild == guild
		mt := mob.target
		elixir := true
		if dam == -1 {
			d, ok := g.skillOnTarget(u, &p, i, tid, &sc, master, sameSide)
			if !ok {
				continue
			}
			dam = d
			elixir = skill > 104 || skill < 0 || g.data.Skills[skill].InstanceType != 0
		} else if dam != -2 {
			t.Damage = 0
			continue
		} else {
			dist := distance(int(me.target.X), int(me.target.Y), int(mt.X), int(mt.Y))
			dist2 := distance(int(me.target.X), int(me.target.Y), int(mob.last.X), int(mob.last.Y))
			if (dist > u.attackRange+3 && dist2 > u.attackRange) || dist > 23 {
				continue
			}
			if i == 0 {
				crit = doubleCritical(&me.combat, pl.Critical, &p.AttackCount)
			}
			dam = int(pl.Status.Attack)
			if crit&2 != 0 {
				if tid < MaxPlayer {
					dam = (rnd()%2 + 13) * dam / 10
				} else {
					dam = (rnd()%2 + 15) * dam / 10
				}
			}
			def := int(mob.info.Player.Status.Defense)
			if tid < MaxPlayer {
				def *= 3
			}
			dam = getDamage(dam, def, master)
			if i == 0 && p.Header.Size >= attackSize1 && pl.ClassInfo == 3 && pl.Learn[0]&0x200000 != 0 && rnd()%4 == 0 {
				extra := (int(pl.Status.DEX) + int(pl.Status.Mastery[3])) >> 2 / 2
				if pl.Learn[0]&0x800000 != 0 {
					extra += extra * 5 / 100
				}
				if mob.info.Player.Status.Level >= 300 {
					extra = extra * (100 - int(mob.info.Player.Learn[0])) / 100
				}
				p.Target[1] = wire.AttackTarget{Damage: int64(extra)}
				crit |= 4
				dam += extra
			}
			if crit&1 != 0 {
				dam *= 2
			}
			p.DoubleCritical = crit
		}
		t.Damage = int64(dam)
		if dam <= 0 {
			continue // misses, heals and pure buffs carry no damage
		}

		// Damage elixirs (affect 30) and caps: mobs only.
		if tid >= MaxPlayer && elixir {
			for _, a := range me.info.Affects {
				if a.Index == 30 {
					switch a.Value {
					case 1, 3:
						dam += 2000
					case 2:
						dam += 500
					}
					break
				}
			}
			dam = min(dam, int(g.cfg.Limits.MaxDamage))
		}
		pvp := tid < MaxPlayer || mob.info.Player.CapeInfo == 4
		if pvp { // players (and summons) take a quarter
			if crit&4 != 0 {
				extra := int(p.Target[1].Damage)
				dam = extra + (dam-extra)>>2
			} else {
				dam >>= 2
			}
		}
		if f := me.combat.forceDamage; f != 0 {
			if dam == 1 {
				dam = f
			} else if dam > 0 {
				dam += f
			}
		}
		if mobLeader == leader || mobGuild == guild {
			dam = 0
		}
		if c1, c2 := pl.CapeInfo, mob.info.Player.CapeInfo; tid >= MaxPlayer && ((c1 == 7 && c2 == 7) || (c1 == 8 && c2 == 8)) {
			dam = 0
		}
		summoner := tid
		if s := mob.npc.summoner; tid >= MaxPlayer && mob.info.Player.CapeInfo == 4 && s > 0 && s < MaxPlayer && g.users[s].status == userPlay {
			summoner = s
		}
		if summoner < MaxPlayer {
			if dam > 0 {
				g.markGuilty(u.id)
			}
			parry := g.parryRate(u.id, tid, orig)
			if (skill == 79 || skill == 22) && i < 3 {
				parry = parry * 30 / 100
			}
			if r := rnd()%1000 + 1; r < parry {
				if sc.maxTarget != 0 {
					dam = -3
				}
				if mob.info.AffectInfo&128 != 0 && r < 100 {
					dam = -4
				}
			}
		}
		g.weaponProcs(u, tid, skill, i)
		if dam > 0 && g.isBossMob(tid) {
			g.markGuiltyAnywhere(u.id)
		}
		if pvp || mob.info.Player.Info&0x3F != 0 { // no fighting in villages / outside PvP areas
			ta := g.data.attribute(int(mt.X), int(mt.Y))
			ma := g.data.attribute(int(me.target.X), int(me.target.Y))
			if ta&attrVillage != 0 || ta&attrPvP == 0 || ma&attrVillage != 0 || ma&attrPvP == 0 {
				dam = 0
			}
		}
		if mob.info.Player.CapeInfo == 6 && pl.Status.Level < 399 {
			dam = 0
		}
		t.Damage = int64(dam)
		if dam <= 0 {
			continue
		}
		if tid < MaxPlayer {
			if r := mob.combat.reflectDamage; r > 0 {
				dam -= r
				if dam <= 0 {
					t.Damage = 0
					continue
				}
			}
			if petAlive(&mob.info.Player) && mob.info.Player.Equip[14].Index >= 2360 && mob.info.Player.Equip[14].Index <= 2389 {
				left := absorbByPet(&mob.info.Player, dam)
				g.processAdultMount(tid, dam-left)
				dam = left
			}
			if mt.X >= 1664 && mt.X <= 1791 && mt.Y >= 1537 && mt.Y <= 1920 {
				dam /= 2
			}
			var absorbed bool
			if dam, _, absorbed = g.manaShield(tid, dam); absorbed {
				g.sendScore(tid)
			}
			t.Damage = int64(dam)
		}
		taken := dam
		if hp := mob.info.Player.Equip[13].Index; hp == 786 || hp == 1936 || hp == 1937 {
			sanc := float32(max(itemSanc(&mob.info.Player.Equip[13]), 2))
			mult := float32(1)
			switch hp {
			case 1936:
				mult = 10
			case 1937:
				mult = 1000
			}
			taken = int(float64(dam) / float64(mult*sanc))
		} else if taken > int(mob.info.Player.Status.CurHP) {
			taken = int(mob.info.Player.Status.CurHP)
		}
		mob.info.Player.Status.CurHP -= int64(taken)
		if tid >= MaxPlayer {
			g.linkMountHP(tid)
		}

		perc := me.combat.lifeSteal
		if orig == -1 { // magic
			perc = me.combat.vampirism
		}
		if skill == 79 && i != 0 {
			perc = 0
		}
		if heal := dam * perc / 100; heal > 0 {
			if tid >= MaxPlayer {
				heal = min(heal/4, 300)
			}
			pl.Status.CurHP = min(pl.Status.CurHP+int64(heal), pl.Status.MaxHP)
			g.multicast2(int(me.target.X), int(me.target.Y), &wire.HpChange{
				Header: wire.NewHeader(wire.OpSrvHpChange, uint16(u.id)), CurHP: pl.Status.CurHP, Incress: int64(heal)}, 0)
		}
		// TransKnight aura of life (affect 17): every 5th attack heals 20%.
		if pl.ClassInfo == 0 && me.combat.attackCount%5 == 0 {
			for _, a := range me.info.Affects {
				if a.Index == 17 {
					heal := dam * 20 / 100
					if tid >= MaxPlayer {
						heal /= 4
					}
					heal = min(heal, 300)
					pl.Status.CurHP = min(pl.Status.CurHP+int64(heal), pl.Status.MaxHP)
					g.multicast2(int(me.target.X), int(me.target.Y), &wire.HpChange{
						Header: wire.NewHeader(wire.OpSrvHpChange, uint16(u.id)), CurHP: pl.Status.CurHP, Incress: int64(heal)}, 0)
					break
				}
			}
		}
		// BeastMaster with both shield skills (bits 19 and 23) reflects 10%.
		if tp := &mob.info.Player; tid < MaxPlayer && tp.ClassInfo == 2 && tp.Learn[0]&(1<<19) != 0 && tp.Learn[0]&(1<<23) != 0 {
			reflect := min(dam/10, 350)
			if skill == 79 && i != 0 {
				reflect = 0
			}
			if reflect > 0 {
				pl.Status.CurHP = max(pl.Status.CurHP-int64(reflect), 0)
				g.multicast2(int(me.target.X), int(me.target.Y), &wire.HpChange{
					Header: wire.NewHeader(wire.OpSrvHpChange, uint16(u.id)), CurHP: pl.Status.CurHP, Incress: int64(-reflect)}, 0)
				if pl.Status.CurHP <= 0 {
					g.mobKilled(u.id, tid)
				}
			}
		}
		if mob.info.Player.Status.CurHP <= 0 {
			mob.info.Player.Status.CurHP = 0
			continue
		}
		g.setBattle(tid, u.id)
		me.combat.currentTarget = tid
		g.wakeGroup(tid, u.id)
		g.summonsFight(u.id, tid, false) // SendUpdateApoioEvoc + EvocaList
		if tid < MaxPlayer {
			g.summonsFight(tid, u.id, false)
		}
	}
	if pl.Learn[0]&0x20000000 != 0 {
		p.DoubleCritical |= 8
	}
	if skill == 30 {
		g.sendSetHpMp(u)
	}
	p.CurrentExp = pl.Exp
	p.CurrentMP = pl.Status.CurMP
	p.Hold = int64(me.info.Hold)
	p.Header.ClientID = uint16(u.id)
	raw, err := wire.Marshal(&p)
	if err != nil {
		return
	}
	g.multicastRaw(int(me.target.X), int(me.target.Y), raw[:p.Header.Size])

	for i := 0; i < maxTarget; i++ {
		tid := int(p.Target[i].Index)
		if tid <= 0 || tid >= MaxMob || g.mobs[tid].mode == 0 {
			continue
		}
		if g.mobs[tid].info.Player.Status.CurHP == 0 {
			g.mobKilled(tid, u.id)
		} else {
			g.getCurrentScore(tid)
		}
	}
	g.checkGetLevel(u)
}

// sides returns RequestAttack's leader/guild pair for attacker and target
// (guild -1 when neither has one, so "no guild" never matches).
func (g *Game) sides(att, tid int) (leader, mobLeader, guild, mobGuild int) {
	me, mob := &g.mobs[att], &g.mobs[tid]
	leader, mobLeader = att, tid
	if me.npc.leader != 0 {
		leader = me.npc.leader
	}
	if mob.npc.leader != 0 {
		mobLeader = mob.npc.leader
	}
	guild, mobGuild = int(me.info.Player.GuildIndex), int(mob.info.Player.GuildIndex)
	if me.guildDisable || (mob.target.X/128 == 1 && mob.target.Y/128 == 4) {
		guild = 0
	}
	if mob.guildDisable {
		mobGuild = 0
	}
	if guild == 0 && mobGuild == 0 {
		guild = -1
	}
	return
}
