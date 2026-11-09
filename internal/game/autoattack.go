package game

import "dbsrv/internal/wire"

// The attacks the server makes for a player (ProcessAffect in Basedef.cpp):
// the familiar (items 754, 769 and 1726) and the auras 22 and 23. In the
// C++ they are packets fed back into RequestAttack with a magic timestamp,
// which skips the attack rate, cooldown, learned and mana checks; here they
// call doAttack directly.
//
// Only the player side is ported. The C++ also let mobs with aura 22 hit
// around them, with a separate damage formula.

// autoAttack makes a player attack the targets with skill.
func (g *Game) autoAttack(u *User, skill int, motion uint8, size uint16, targets []int) {
	me := &g.mobs[u.id]
	op := uint16(0x367)
	if size == attackSize1 {
		op = 0x39D
	}
	p := wire.Attack{Header: wire.NewHeader(op, uint16(u.id)), AttackerPos: me.target,
		AttackerID: int16(u.id), Motion: motion, SkillID: int16(skill)}
	p.Header.Size = size
	for i, t := range targets {
		p.Target[i] = wire.AttackTarget{Index: int32(t), Damage: -1}
	}
	g.doAttack(u, p, true)
}

// inView reports a target within the view of id.
func (g *Game) inView(id, t int) bool {
	a, b := g.mobs[id].target, g.mobs[t].target
	return int(b.X) >= int(a.X)-viewGrid && int(b.X) <= int(a.X)+viewGrid &&
		int(b.Y) >= int(a.Y)-viewGrid && int(b.Y) <= int(a.Y)+viewGrid
}

// familiarAttack is the head of ProcessAffect: a familiar hits what its
// owner is fighting. The C++ checked the owner's mode where it meant the
// target's, and attacked once more after clearing an ally as the target.
func (g *Game) familiarAttack(u *User) {
	me := &g.mobs[u.id]
	skill := 0
	switch me.info.Player.Equip[13].Index {
	case 754:
		skill = 32
	case 769:
		skill = 34
	case 1726:
		skill = 36
	default:
		return
	}
	t := me.combat.currentTarget
	if t == 0 {
		return
	}
	clear := func() { me.combat.currentTarget = 0 }
	if t == u.id || t < 0 || t >= MaxMob || g.mobs[t].mode == 0 {
		clear()
		return
	}
	o := &g.mobs[t]
	if g.data.attribute(int(me.target.X), int(me.target.Y))&attrPvP != 0 && (t < MaxPlayer || o.info.Player.CapeInfo == 4) && !u.pkMode {
		clear()
		return
	}
	if t < MaxPlayer && g.data.attribute(int(o.target.X), int(o.target.Y))&attrPvP == 0 {
		clear()
		return
	}
	leader, otherLeader, guild, otherGuild := g.sides(u.id, t)
	if leader == otherLeader || guild == otherGuild {
		clear()
		return
	}
	sameKingdom := me.info.Player.CapeInfo == o.info.Player.CapeInfo && (o.info.Player.CapeInfo == 7 || o.info.Player.CapeInfo == 8)
	if sameKingdom && !u.pkMode && t >= MaxPlayer {
		return
	}
	if !g.inView(u.id, t) {
		clear()
		return
	}
	g.autoAttack(u, skill, 0xFE, attackSize1, []int{t})
}

// alliedTo reports the same leader, guild or an allied guild.
func (g *Game) alliedTo(id, o int) bool {
	leader, otherLeader, guild, otherGuild := g.sides(id, o)
	ally := g.guildAlly[guild]
	if ally == 0 {
		ally = -2
	}
	return leader == otherLeader || guild == otherGuild || ally == otherGuild
}

// auraSkips is what both auras leave out around the player.
func (g *Game) auraSkips(u *User, o int) bool {
	t := &g.mobs[o]
	pl := &t.info.Player
	if o == u.id || t.mode == 0 || pl.CapeInfo == 6 {
		return true
	}
	mine := g.mobs[u.id].info.Player.CapeInfo
	if o >= MaxPlayer && mine == pl.CapeInfo && (mine == 7 || mine == 8) {
		return true
	}
	if g.alliedTo(u.id, o) {
		return true
	}
	if pl.AffectInfo&affSnoop != 0 || pl.Info&1 != 0 {
		return true
	}
	return false
}

// gridTargets lists the ids in the square of the given radius around id.
func (g *Game) gridTargets(id, radius int, visit func(o int) bool) {
	me := g.mobs[id].target
	for y := int(me.Y) - radius; y <= int(me.Y)+radius; y++ {
		if y < 0 || y >= gridSize {
			continue
		}
		for x := int(me.X) - radius; x <= int(me.X)+radius; x++ {
			if x < 0 || x >= gridSize {
				continue
			}
			if o := int(g.cell(x, y)); o > 0 && o < MaxMob && !visit(o) {
				return
			}
		}
	}
}

// auraOfFire is affect 22: up to 6 targets within 1 cell, then 2. How many
// are hit grows with the affect value and the level (skill 33).
func (g *Game) auraOfFire(u *User, value, level int) {
	me := &g.mobs[u.id]
	x, y := int(me.target.X), int(me.target.Y)
	if g.data.attribute(x, y)&attrVillage != 0 {
		return
	}
	pvpHere := g.data.attribute(x, y)&attrPvP != 0
	leader := u.id
	if me.npc.leader > 0 {
		leader = me.npc.leader
	}
	var found []int
	collect := func(o int) bool {
		t := &g.mobs[o]
		if t.info.Player.Status.CurHP <= 0 || t.npc.leader == leader || g.auraSkips(u, o) {
			return true
		}
		isPlayerSide := o < MaxPlayer || t.info.Player.CapeInfo == 4
		if isPlayerSide && (!u.pkMode || !pvpHere) {
			return true
		}
		for _, f := range found {
			if f == o {
				return true
			}
		}
		found = append(found, o)
		return len(found) < 6
	}
	g.gridTargets(u.id, 1, collect)
	if len(found) < 6 {
		g.gridTargets(u.id, 2, collect)
	}
	if len(found) == 0 {
		return
	}
	power := value + level + rnd()%100
	count := 1
	for _, need := range []int{300, 350, 400, 450, 500} {
		if power > need {
			count++
		}
	}
	motion := uint8(0xFE)
	if me.info.Player.Equip[0].Index == 219 {
		motion = 0xFC
	}
	g.autoAttack(u, 33, motion, attackSize, found[:min(count, len(found))])
}

// auraBestial is affect 23: what is next to the player, or else what it is
// fighting (skill 52). The current target is dropped when the player cannot
// hit it any more; a target slowed down holds the aura back.
func (g *Game) auraBestial(u *User) {
	me := &g.mobs[u.id]
	x, y := int(me.target.X), int(me.target.Y)
	if g.data.attribute(x, y)&attrVillage != 0 {
		return
	}
	pvpHere := g.data.attribute(x, y)&attrPvP != 0
	current := 0
	if t := me.combat.currentTarget; t != 0 {
		switch {
		case t == u.id || t < 0 || t >= MaxMob || g.mobs[t].mode == 0:
			me.combat.currentTarget = 0
		case g.alliedTo(u.id, t):
		case t >= MaxPlayer && !u.pkMode && g.mobs[t].npc.summoner != 0:
		case t < MaxPlayer && (!u.pkMode || !pvpHere):
		case t < MaxPlayer && (g.mobs[t].info.Player.AffectInfo&affSnoop != 0 || g.mobs[t].info.Player.Info&1 != 0):
			me.combat.currentTarget = 0
		case t < MaxPlayer && g.mobs[t].info.Player.AffectInfo&affSlowMov != 0:
			return
		case !g.inView(u.id, t):
			me.combat.currentTarget = 0
		default:
			current = t
		}
	}
	var found []int
	g.gridTargets(u.id, 1, func(o int) bool {
		if g.auraSkips(u, o) {
			return true
		}
		if !u.pkMode && (o < MaxPlayer || g.mobs[o].npc.summoner != 0) {
			return true
		}
		if !pvpHere && o < MaxPlayer {
			return true
		}
		found = append(found, o)
		return true
	})
	if len(found) == 0 && current != 0 {
		found = []int{current}
	}
	if len(found) > 0 {
		g.autoAttack(u, 52, 0xFE, attackSize, found)
	}
}

// ---- skill specials (RequestAttack.cpp) ----

// furyPull is Fúria Divina (6): pulls the target next to the caster.
func (g *Game) furyPull(u *User, tid int) {
	me := &g.mobs[u.id]
	mob := &g.mobs[tid]
	pl, tp := &me.info.Player, &mob.info.Player
	if (tid >= MaxPlayer && tp.Info&0x3F != 0) || tp.CapeInfo == 6 {
		return
	}
	switch tp.Equip[0].Index {
	case 219, 220, 357, 362, 397:
		return
	}
	if mob.npc.generateID == 374 {
		return
	}
	mob.route[0] = 0
	x, y := int(me.target.X), int(me.target.Y)
	if x < int(mob.target.X) {
		x++
	} else if x > int(mob.target.X) {
		x--
	}
	if y < int(mob.target.Y) {
		y++
	} else if y > int(mob.target.Y) {
		y--
	}
	if !g.emptyMobGrid(tid, &x, &y) {
		return
	}
	mastery := int(pl.Status.Mastery[1])
	chance := mastery/10 + 40
	if tid >= MaxPlayer {
		chance = mastery/5 + 60
	}
	mine, theirs := int(pl.Status.Level), int(tp.Status.Level)
	if evolution(pl) >= evoCelestial {
		mine += 300
	}
	if tid < MaxPlayer {
		if evolution(tp) >= evoCelestial {
			theirs += 300
		}
		theirs += totalResets(mob.info.Info) * 50
		mine += totalResets(me.info.Info) * 50
	}
	if rnd()%100 >= chance+(mine-theirs)>>1 {
		return
	}
	sm := g.action(tid, x, y)
	sm.MoveType, sm.MoveSpeed = 2, 6
	g.multicastMove(tid, x, y, &sm)
	if tu := g.player(tid); tu != nil {
		g.queue(tu, &sm)
		g.markGuilty(u.id)
	}
}

// soulLimit is Limite da Alma (102): false when the Arch cannot use it.
func (g *Game) soulLimit(u *User) bool {
	me := &g.mobs[u.id]
	pl := &me.info.Player
	slot := g.emptyAffect(u.id, 29)
	if slot < 0 || evolution(pl) == evoArch {
		return false
	}
	ticks := uint32(50)
	if evolution(pl) >= evoCelestial {
		ticks = uint32((int(pl.BStatus.Level) + 25) * 3 / 8)
	}
	me.info.Affects[slot] = wire.Affect{Index: 29, Master: 29, Value: 10, Time: ticks}
	g.sendAffect(u)
	return true
}

// teleportBuff is Teleporte (41): the buff reaches a few of the targets.
func (g *Game) teleportBuff(p *wire.Attack, sc *skillCast) {
	n := min(max(sc.mastery/25+2, 2), 13)
	for l := 0; l < n; l++ {
		id := int(p.Target[l].Index)
		if id <= 0 || id >= MaxPlayer || g.users[id].status != userPlay {
			continue
		}
		if g.setAffect(id, sc.skill, sc.delay, sc.mastery) {
			g.sendScore(id)
		}
	}
}

// thornWall is Muro de Espinhos (98): a Vinha that lasts a few ticks. The
// C++ took the position from the packet unchecked.
func (g *Game) thornWall(u *User, p *wire.Attack) bool {
	x, y := int(p.TargetPos.X), int(p.TargetPos.Y)
	me := g.mobs[u.id].target
	if g.data.Thorn == nil || x <= 0 || y <= 0 || x >= gridSize || y >= gridSize ||
		x < int(me.X)-viewGrid || x > int(me.X)+viewGrid || y < int(me.Y)-viewGrid || y > int(me.Y)+viewGrid ||
		g.data.attribute(x, y)&attrVillage != 0 {
		return false
	}
	id := g.emptyNPCMob()
	if id == 0 {
		return false
	}
	m := &g.mobs[id]
	*m = Mob{}
	m.info.Player = *g.data.Thorn
	pl := &m.info.Player
	for i := range pl.Name {
		if pl.Name[i] == '_' || pl.Name[i] == '@' {
			pl.Name[i] = ' '
		}
	}
	m.npc.generateID, m.npc.routeType, m.npc.waitSec = -1, 5, 10
	for s := range m.npc.segX {
		m.npc.segX[s], m.npc.segY[s] = x, y
	}
	g.getCurrentScore(id)
	pl.Status.CurHP = pl.Status.MaxHP
	if !g.emptyMobGrid(id, &x, &y) {
		*m = Mob{}
		return false
	}
	m.info.Affects[0] = wire.Affect{Index: affSummoned, Time: 10}
	g.placeNPC(id, x, y)
	return true
}

// familiarLosesRefine: dying costs familiars 753 and 1726 one refine, and
// at +0 the familiar itself.
func (g *Game) familiarLosesRefine(id int) {
	it := &g.mobs[id].info.Player.Equip[13]
	if it.Index != 753 && it.Index != 1726 {
		return
	}
	if sanc := itemSanc(it); sanc <= 0 {
		g.log.Info("familiar destroyed", "slot", id, "item", it.Index)
		*it = wire.Item{}
	} else {
		for e := range it.Effect {
			if it.Effect[e].Index == EF_SANC {
				it.Effect[e].Value = uint16(sanc - 1)
				break
			}
		}
	}
	if u := g.player(id); u != nil {
		g.sendItem(u, wire.SlotEquip, 13, it)
	}
}
