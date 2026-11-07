package game

import (
	"math"

	"dbsrv/internal/wire"
)

// Summons: the BeastMaster evocations (skills 56..63, instance type 11) and
// the baby mounts that walk next to their owner (SendFunc.cpp
// GenerateSummon / SendPartyEvocation, Basedef.cpp MountProcess /
// ProcessAdultMount / LinkMountHp, CMob.cpp StandingByProcessor and
// BattleProcessor for route type 5, the EvocaList loops of
// ProcessSecTimer / RequestAttack / MobKilled).
//
// A summon is an NPC slot (mode 4/5, CapeInfo 4, route type 5) whose
// npc.summoner and npc.leader are its owner; the owner lists it in
// Mob.evoca (EvocaList[36]). Evocations live 30 (Lobisomem.. 128) ticks
// through affect 24 in Affects[0]; baby mounts (faces 315..344) live while
// the mount is equipped and alive, their HP mirrored on the item.

const (
	maxEvoca            = 36
	opSrvPartyEvocation = 0x3EA
	opSrvSummonTime     = 0xEE1 // pMsgSignal: seconds left (1 = gone)
	affSummoned         = 24    // Affects[0] of an evocation: its lifetime
	mountSummonFirst    = 315   // baby mount summon faces 315..344
	mountSummonEnd      = 345
	firstEvocationSkill = 56
)

// PartyEvocation mirrors MSG_PartyEvocation (88 bytes).
type PartyEvocation struct {
	Header wire.Header
	Leader int16
	List   [maxEvoca]int16
	_      [2]uint8
}

// summonBonus is pSummonBonus: attack/defense/HP the evocation gets from
// the caster's INT and BeastMaster mastery (only the eight evocations have
// one; mounts get nothing here).
var summonBonus = [8][6]int{
	{80, 350, 70, 75, 100, 400},
	{80, 300, 70, 150, 125, 400},
	{80, 450, 70, 125, 125, 400},
	{80, 400, 70, 200, 150, 400},
	{80, 550, 70, 175, 150, 400},
	{80, 450, 70, 250, 175, 400},
	{100, 550, 60, 250, 174, 400},
	{130, 300, 80, 200, 180, 250},
}

// mountCon is MountCon: base CON of each baby mount summon (face - 315).
var mountCon = [30]int{0, 800, 900, -800, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 600}

func isMountFace(face uint16) bool { return face >= mountSummonFirst && face < mountSummonEnd }

// summonOwner is the player a summon fights for, 0 if none.
func (g *Game) summonOwner(id int) int {
	if id < MaxPlayer || id >= MaxMob {
		return 0
	}
	m := &g.mobs[id]
	if m.info.Player.CapeInfo != 4 || !m.npc.isSummon {
		return 0
	}
	if s := m.npc.summoner; s > 0 && s < MaxPlayer {
		return s
	}
	return 0
}

// sendPartyEvocation is SendPartyEvocation: the owner's summon list.
func (g *Game) sendPartyEvocation(id int) { g.sendPartyEvocationFlag(id, 1) }

// sendPartyEvocationFlag is SendPartyEvocation(id, indice).
func (g *Game) sendPartyEvocationFlag(id int, leader int16) {
	u := g.player(id)
	if u == nil || u.status != userPlay {
		return
	}
	p := PartyEvocation{Header: wire.NewHeader(opSrvPartyEvocation, uint16(id)), Leader: leader}
	for i, e := range g.mobs[id].evoca {
		p.List[i] = int16(e)
	}
	g.queue(u, &p)
}

// generateSummon is GenerateSummon(user, summonId, item): an NPCBase
// creature next to the user, scaled by the user's INT/mastery (item ==
// nil: an evocation) or by the mount item (its HP and refine).
func (g *Game) generateSummon(user, summonID int, item *wire.Item) bool {
	if summonID < 0 || summonID >= len(g.data.NPCBase) {
		return false
	}
	owner := &g.mobs[user]
	slot := -1
	for i, e := range owner.evoca {
		if e == 0 {
			slot = i
			break
		}
	}
	u := g.player(user)
	if slot < 0 {
		if u != nil {
			g.clientMessage(u, "Não é possível evocar, lista cheia")
		}
		return false
	}
	id := g.emptyNPCMob()
	if id == 0 {
		return false
	}
	x, y := int(owner.target.X), int(owner.target.Y)
	if !g.emptyMobGrid(id, &x, &y) {
		return false
	}
	m := &g.mobs[id]
	*m = Mob{}
	pl := &m.info.Player
	*pl = g.data.NPCBase[summonID]
	op := &owner.info.Player
	pl.BStatus.Level, pl.Status.Level = op.BStatus.Level, op.BStatus.Level
	name := wire.CString(pl.Name[:]) + "^"
	pl.Name = [16]byte{}
	copy(pl.Name[:15], name)
	for i := range pl.Name {
		if pl.Name[i] == '_' {
			pl.Name[i] = ' '
		}
	}

	sINT := int(op.Status.INT) + int(op.Status.INT)*15/100
	sMast := int(op.Status.Mastery[2]) + 50
	if item == nil && op.ClassInfo == 2 && op.Learn[0]&(1<<15) != 0 { // the 8th BeastMaster skill
		sINT += int(math.Ceil(float64(sINT) * 0.105))
		sMast += 50
	}
	if summonID < len(summonBonus) {
		b := summonBonus[summonID]
		pl.BStatus.Attack += int32(b[0]*sINT/100 + b[1]*sMast/100)
		pl.BStatus.Defense += int32(b[2]*sINT/100 + b[3]*sMast/100)
		pl.Status.MaxHP = pl.BStatus.MaxHP + int32(b[4]*sINT/100+b[5]*sMast/100)
	} else {
		pl.Status.MaxHP = pl.BStatus.MaxHP
	}
	pl.BStatus.MaxHP = pl.Status.MaxHP

	n := &m.npc
	n.generateID = -1
	n.formation = 5
	n.routeType = 5
	n.summoner = user
	n.isSummon = true
	n.leader = user
	m.lastTime = g.now()

	if item != nil && isMountFace(pl.Equip[0].Index) {
		sanc := min(g.data.itemAbility(item, EF_MOUNTSANC), 100)
		base := mountCon[int(pl.Equip[0].Index)-mountSummonFirst]
		lo := base/2 - 1000
		con := lo + (base-lo)*sanc/100
		pl.BStatus.CON, pl.Status.CON = uint16(int16(con)), uint16(int16(con))
		pl.BStatus.Attack += int32((g.data.itemAbility(item, EF_MOUNTSANC) & 255) * 6)
		pl.Status.Attack = pl.BStatus.Attack
	}
	if !owner.guildDisable {
		pl.GuildIndex = op.GuildIndex
		pl.GuildMemberType = 0
	}
	pl.Status.CurHP = pl.Status.MaxHP
	switch op.Equip[15].Index {
	case 543, 545:
		pl.Equip[15] = wire.Item{Index: 734}
	case 544, 546:
		pl.Equip[15] = wire.Item{Index: 735}
	case 548, 549:
		pl.Equip[15] = wire.Item{Index: 550}
	}
	pl.CapeInfo = 4
	g.getCurrentScore(id)

	if item == nil {
		life := uint32(30)
		if summonID >= 28 && summonID <= 37 {
			life = 128
		}
		m.info.Affects[0] = wire.Affect{Index: affSummoned, Time: life}
		if u != nil {
			g.signalParm(u, uint16(user), opSrvSummonTime, int32(life))
		}
	} else {
		if hp := mountHP(item); hp > int(pl.Status.MaxHP) {
			setMountHP(item, int(pl.Status.MaxHP))
		}
		pl.Status.CurHP = int32(mountHP(item))
		pl.BStatus.CurHP = pl.Status.CurHP
	}

	owner.evoca[slot] = id
	g.placeNPC(id, x, y)
	g.sendPartyEvocation(user)
	for _, f := range owner.npc.followers {
		if f > 0 && f < MaxPlayer {
			g.sendPartyEvocation(f)
		}
	}
	return true
}

// mountProcess is MountProcess: drop the owner's mount summon and, when a
// living baby mount (2330..2359) is equipped, summon it again with the
// item's current HP and level.
//
// The C++ also summoned a dead mount (0 HP), which the timer then deleted
// at once; only a living one is summoned here.
func (g *Game) mountProcess(id int) {
	m := &g.mobs[id]
	for i, e := range m.evoca {
		if e < MaxPlayer || e >= MaxMob {
			continue
		}
		if s := &g.mobs[e]; s.mode != 0 && s.npc.summoner == id && isMountFace(s.info.Player.Equip[0].Index) {
			m.evoca[i] = 0
			g.deleteMob(e, 3)
		}
	}
	mount := &m.info.Player.Equip[14]
	if k := int(mount.Index) - 2320; k >= 10 && k < 40 && mountHP(mount) > 0 {
		g.generateSummon(id, k, mount)
	}
}

// processAdultMount is ProcessAdultMount: an adult mount (2360..2389)
// takes `damage` (0 = just clamp to its max), dies at 0 HP. The Poção
// Revigorante (affect 49) protects it.
func (g *Game) processAdultMount(id, damage int) {
	m := &g.mobs[id]
	mount := &m.info.Player.Equip[14]
	if mount.Index < 2360 || mount.Index >= 2390 {
		return
	}
	for _, a := range m.info.Affects {
		if a.Index == 49 {
			return
		}
	}
	maxHP := 0
	if i := int(mount.Index) - 2360 + 10; i < len(g.data.NPCBase) {
		maxHP = int(g.data.NPCBase[i].Status.MaxHP)
	}
	if mount.Effect[2].Index == 0 && mount.Effect[1].Index != 0 { // starved
		setMountHP(mount, 0)
	}
	before := mountHP(mount)
	hp := min(before-damage, maxHP)
	setMountHP(mount, hp)
	if hp <= 0 {
		mount.Effect[2].Index = 0
	}
	u := g.player(id)
	if before > 0 && hp <= 0 {
		if u != nil {
			g.sendEquip(u)
		}
		g.log.Info("mount died", "slot", id, "mount", mount.Index, "hit", damage)
	}
	if before != hp && u != nil {
		g.sendItem(u, wire.SlotEquip, 14, mount)
	}
}

// linkMountHP is LinkMountHp: a hit baby mount summon writes its HP back
// to the owner's mount item.
func (g *Game) linkMountHP(id int) {
	owner := g.summonOwner(id)
	if owner == 0 || g.users[owner].status != userPlay {
		return
	}
	face := g.mobs[id].info.Player.Equip[0].Index
	mount := &g.mobs[owner].info.Player.Equip[14]
	if !isMountFace(face) || int(mount.Index)-2330 != int(face)-mountSummonFirst {
		return
	}
	if hp := int(g.mobs[id].info.Player.Status.CurHP); mountHP(mount) != hp {
		setMountHP(mount, hp)
		g.sendItem(g.users[owner], wire.SlotEquip, 14, mount)
	}
}

// removeSummon takes a summon out of its owner's list (DeleteMob of a
// summon; the C++ cleaned the lists lazily).
func (g *Game) removeSummon(id int) {
	owner := g.mobs[id].npc.summoner
	if owner <= 0 || owner >= MaxPlayer {
		return
	}
	for i, e := range g.mobs[owner].evoca {
		if e == id {
			g.mobs[owner].evoca[i] = 0
			if u := g.player(owner); u != nil && u.status == userPlay && !isMountFace(g.mobs[id].info.Player.Equip[0].Index) {
				g.signalParm(u, uint16(owner), opSrvSummonTime, 1)
			}
		}
	}
}

// dismissSummons deletes every summon of a player leaving the world.
func (g *Game) dismissSummons(id int) {
	m := &g.mobs[id]
	for i, e := range m.evoca {
		m.evoca[i] = 0
		if e >= MaxPlayer && e < MaxMob && g.mobs[e].mode != 0 && g.mobs[e].npc.summoner == id {
			g.mobs[e].npc.summoner = 0
			g.deleteMob(e, 3)
		}
	}
}

// summonsFight sends the summons of `who`'s party leader (or who) after
// enemy (the EvocaList loops of RequestAttack / ProcessSecTimer); dead
// entries are dropped. back also turns the enemy on each summon.
func (g *Game) summonsFight(who, enemy int, back bool) {
	if who <= 0 || who >= MaxPlayer {
		return
	}
	lead := who
	if l := g.mobs[who].npc.leader; l > 0 && l < MaxPlayer {
		lead = l
	}
	for _, owner := range []int{lead, who} {
		for i, e := range g.mobs[owner].evoca {
			if e < MaxPlayer || e >= MaxMob {
				continue
			}
			s := &g.mobs[e]
			if s.mode == 0 || s.info.Player.Status.CurHP <= 0 {
				if s.mode != 0 {
					g.deleteMob(e, 1)
				}
				g.mobs[owner].evoca[i] = 0
				continue
			}
			if enemy == e || g.summonOwner(enemy) != 0 {
				continue // summons do not fight summons (SendUpdateApoioEvoc)
			}
			g.setBattle(e, enemy)
			if back {
				g.setBattle(enemy, e)
			}
		}
		if owner == who {
			break
		}
	}
}

// summonStandingBy is the route type 5 part of StandingByProcessor: follow
// the owner (teleport beyond 20 cells), vanish when the owner is gone, left
// the party or the evocation's time ran out.
func (g *Game) summonStandingBy(id int) int {
	m := &g.mobs[id]
	n := &m.npc
	face := m.info.Player.Equip[0].Index
	if face == 358 {
		if m.info.Affects[0].Index != affSummoned {
			return aiKeepAway
		}
		return 0
	}
	owner := n.summoner
	if owner <= 0 || owner >= MaxPlayer || n.leader <= 0 || n.leader >= MaxPlayer {
		return aiKeepAway
	}
	if m.info.Affects[0].Index != affSummoned && !isMountFace(face) {
		return aiKeepAway
	}
	if !g.sameParty(n.leader, owner) || g.users[owner].status != userPlay {
		return aiKeepAway
	}
	o := &g.mobs[owner]
	d := distance(int(m.target.X), int(m.target.Y), int(o.target.X), int(o.target.Y))
	switch {
	case d >= 20:
		n.nextX, n.nextY = int(o.target.X), int(o.target.Y)
		return aiTeleport
	case d > 2:
		n.curX, n.curY = int(o.target.X), int(o.target.Y)
		if nx, ny, ok := g.nextPos(id, false); ok && (nx != int(m.target.X) || ny != int(m.target.Y)) {
			n.nextX, n.nextY = nx, ny
			return aiMove
		}
	}
	return 0
}

// sameParty reports whether member is leader or in leader's party.
func (g *Game) sameParty(leader, member int) bool {
	if leader == member {
		return true
	}
	for _, f := range g.mobs[leader].npc.followers {
		if f == member {
			return true
		}
	}
	return false
}

// summonBattle is the route type 5 part of BattleProcessor: 0 to fight on,
// aiDelete when the owner is gone, aiTeleport to catch up beyond 20 cells.
// (The C++ returned 256 - "keep away" - for a vanished owner, which kept
// the summon fighting forever.)
func (g *Game) summonBattle(id int) int {
	m := &g.mobs[id]
	n := &m.npc
	if m.info.Player.Equip[0].Index == 358 {
		if m.info.Affects[0].Index != affSummoned {
			return aiDelete
		}
		return 0
	}
	owner := n.summoner
	if owner <= 0 || owner >= MaxPlayer || n.leader <= 0 || n.leader >= MaxPlayer ||
		!g.sameParty(n.leader, owner) || g.users[owner].status != userPlay {
		return aiDelete
	}
	o := &g.mobs[owner]
	if distance(int(m.target.X), int(m.target.Y), int(o.target.X), int(o.target.Y)) >= 20 {
		n.nextX, n.nextY = int(o.target.X), int(o.target.Y)
		return aiTeleport
	}
	return 0
}

// castEvocation is the instance type 11 branch of RequestAttack: skills
// 56..63 call NPCBase[skill - 56], as many as the BeastMaster mastery
// allows (one more with the 8th skill), replacing nothing: only the
// missing ones are summoned, and only one kind at a time.
func (g *Game) castEvocation(u *User, skill int) {
	if !g.evocationAllowed(u) {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	kind := skill - firstEvocationSkill
	if kind < 0 || kind > 7 {
		return
	}
	mast := int(pl.Status.Mastery[2])
	count := [8]int{mast / 30, mast / 30, mast / 40, mast / 40, mast / 40, mast / 80, mast / 80, 2}[kind]
	t := m.target
	if (t.X >= 322 && t.X <= 352 && t.Y >= 416 && t.Y <= 454) || (t.X >= 1030 && t.X <= 1150 && t.Y >= 1409 && t.Y <= 1535) {
		g.clientMessage(u, "Não é possível evocar nesta area")
		return
	}
	if count == 0 {
		count = 1
	}
	face := g.data.NPCBase[kind].Equip[0].Index
	have := 0
	for _, e := range m.evoca {
		if e < MaxPlayer || e >= MaxMob || g.mobs[e].mode == 0 {
			continue
		}
		s := &g.mobs[e]
		ef := s.info.Player.Equip[0].Index
		if s.npc.summoner != u.id {
			if !isMountFace(ef) {
				return // a party member's evocation is here
			}
			continue
		}
		if isMountFace(ef) {
			continue
		}
		if ef != face {
			return // another kind is out
		}
		have++
	}
	if pl.Learn[0]&(1<<15) != 0 {
		count++
	}
	for range count - have {
		if !g.generateSummon(u.id, kind, nil) {
			break
		}
	}
}

// mountKillExp is the MobKilled part where a baby mount summon that kills a
// mob gains kill points and, every 25..100 kills, a level (up to 100).
func (g *Game) mountKillExp(killer, dead int) {
	owner := g.summonOwner(killer)
	if owner == 0 || !isMountFace(g.mobs[killer].info.Player.Equip[0].Index) || g.mobs[dead].info.Player.CapeInfo == 4 {
		return
	}
	u := g.player(owner)
	if u == nil || u.status != userPlay {
		return
	}
	mount := &g.mobs[owner].info.Player.Equip[14]
	if mount.Index < 2330 || mount.Index >= 2360 {
		return
	}
	level, kills := int(mount.Effect[1].Index), int(mount.Effect[2].Value)
	need := (level + 100) & 255
	if d := int(mount.Index) - 2330; d >= 0 && d <= 5 {
		need = level + 25 + 10*d
	}
	if level >= int(g.mobs[dead].info.Player.Status.Level) || level >= 100 {
		return
	}
	kills++
	if kills >= need {
		mount.Effect[2].Value = 1
		mount.Effect[1].Index = uint8(level + 1)
		g.clientMessage(u, "Sua montaria subiu de level.")
		g.sendItem(u, wire.SlotEquip, 14, mount)
		g.mountProcess(owner)
		return
	}
	mount.Effect[2].Value = uint8(kills)
	g.sendItem(u, wire.SlotEquip, 14, mount)
}
