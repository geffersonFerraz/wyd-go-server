package game

import "dbsrv/internal/wire"

// Mob death, experience and level up (Basedef.cpp MobKilled, GetFunc.cpp
// GetExpApply/GetExpApply_2, CMob.cpp CheckGetLevel).

// expApply is GetExpApply: scale a mob's exp by the level difference and
// the attacker's exp bonus.
func (g *Game) expApply(exp int64, attacker, target int) int64 {
	a, t := &g.mobs[attacker].info.Player, &g.mobs[target].info.Player
	al, tl := int64(a.Status.Level), int64(t.Status.Level)
	ev := int64(evolution(a))
	if (ev <= evoArch && al >= maxLevel) || (ev >= evoCelestial && al >= maxLevelCelestial) {
		return 0
	}
	if al >= 400 || tl >= 400 || al < 0 || tl < 0 {
		return exp
	}
	al++
	tl++
	if ev >= evoCelestial {
		al += 400 + (ev-evoArch)*50
	}
	mult := tl * 100 / al
	switch {
	case mult < 80 && al >= 50:
		mult = (mult-80)*2 + 100
	case mult > 200:
		mult = 200
	case mult < 0:
		mult = 0
	}
	exp = exp * mult / 100
	if exp <= 0 {
		return 0
	}
	exp = g.nightmareExp(exp, attacker)
	if b := g.mobs[attacker].combat.expBonus; b != 0 {
		exp += exp * int64(b) / 100
	}
	if g.kefra.dead {
		exp += exp / 100 * 50
	}
	if g.expRate != 0 {
		exp += exp * int64(g.expRate) / 100
	}
	return exp
}

// expApply2 is GetExpApply_2: level caps and blocked levels (the killer's
// too), the zone/evolution table (expbonus.go), the killer's experience
// chest (affect 39, x2, only with box) and the killer's fairy bonus.
func (g *Game) expApply2(exp int64, receiver, attacker int, box bool) int64 {
	if exp == 0 || g.levelBlocked(attacker) || g.levelBlocked(receiver) {
		return 0
	}
	pl := &g.mobs[receiver].info.Player
	level, ev := int(pl.Status.Level), evolution(pl)
	if (ev >= evoCelestial && level >= maxLevelCelestial) || (ev >= evoArch && level >= maxLevel) {
		return 0
	}
	// Fixed: a reduction above 100% (e.g. "Deserto Arch = 120", or Água
	// above 50) made the C++ take experience away on every kill.
	exp = max(g.zoneExp(exp, receiver), 0)
	a := &g.mobs[attacker].info
	if box && hasAffect(a, 39) {
		exp *= 2
	}
	return exp + fairyExpBonus(exp, a.Player.Equip[13].Index)
}

// levelBlocked is checkIsBlocked of GetExpApply_2: a character waiting at a
// blocked level gets no exp until a compounder unlocks it.
func (g *Game) levelBlocked(id int) bool {
	if id <= 0 || id >= MaxMob {
		return false
	}
	m := &g.mobs[id]
	level, ev := int(m.info.Player.Status.Level), evolution(&m.info.Player)
	switch {
	case m.info.Info&questLvBlocked != 0:
		return ev >= evoCelestial && (level == 39 || level == 89 || level == 199 || level == 209)
	case m.info.Info&questLvBlockedArch != 0:
		return ev >= evoArch && (level == 354 || level == 369)
	}
	return false
}

// mobKilled is MobKilled(target, killer) for an NPC killed by a player.
// TODO(phase C): party share, drops, player deaths, special mobs.
func (g *Game) mobKilled(target, killer int) {
	if target <= 0 || target >= MaxMob || killer <= 0 || killer >= MaxMob || g.mobs[target].mode == 0 {
		return
	}
	if g.arenaMobKilled(target, killer) {
		return
	}
	if g.warLifeSaved(target) || g.specialMobKilled(target, killer) {
		return
	}
	// Whoever dies on the altar during the castle war is moved off it.
	if t := g.mobs[target].target; target < MaxPlayer && g.war.castleState > castleClosing && t.X == altarX && t.Y == altarY {
		g.teleportar(target, 1057, 1742)
	}
	dead := &g.mobs[target]
	// A summon's kill counts for its owner (and trains a baby mount).
	if owner := g.summonOwner(killer); owner != 0 && target >= MaxPlayer {
		g.mountKillExp(killer, target)
		if g.users[owner].status == userPlay {
			killer = owner
		}
	}
	k := &g.mobs[killer]
	if ku := g.player(killer); ku != nil && ku.status == userPlay {
		g.handleMobDrop(ku, mobName(&dead.info.Player))
	}
	dead.info.Player.Status.CurHP = 0
	msg := wire.Killed{
		Header: wire.NewHeader(wire.OpSrvKilled, wire.ClientIDSystem),
		Killed: int16(target), Killer: int16(killer),
	}
	if target < MaxPlayer {
		g.rvrKill(target, killer)
		g.playerKilled(target, killer, &msg)
		return
	}
	u := g.player(killer)
	if u == nil || u.status != userPlay || dead.info.Player.CapeInfo == 4 {
		msg.Hold, msg.Exp = int64(k.info.Hold), k.info.Player.Exp
		g.multicast2(int(dead.target.X), int(dead.target.Y), &msg, 0)
		if target >= MaxPlayer {
			g.deleteMob(target, 1)
		}
		return
	}

	g.shareExp(func(id int) int64 { return g.expApply(dead.info.Player.Exp, id, target) }, killer, dead.target)
	g.missionMobKilled(killer, target)
	g.bigCuboKill(u)
	g.mobGoldDrop(u, dead)
	g.rateDrop(u, dead)

	// The client takes 0x338's Exp as ITS OWN exp when the killer is itself
	// or a party member (TMFieldScene::OnPacketCNFMobKill -> SetMyHumanExp,
	// which shows "Exp +N" from the difference). The C++ sent everyone the
	// killer's exp from before the reward, so party members saw the
	// killer's exp as theirs: huge gains, then huge negative ones. Each
	// party member now gets its own exp after the reward.
	msg.Hold, msg.Exp = int64(k.info.Hold), k.info.Player.Exp
	g.multicastKill(int(dead.target.X), int(dead.target.Y), msg, g.partyMembers(killer))
	g.deleteMob(target, 1)
}

// multicastKill is the 0x338 GridMulticast with each party member's own
// exp and hold.
func (g *Game) multicastKill(x, y int, msg wire.Killed, party []int) {
	own := map[int]bool{}
	for _, id := range party {
		own[id] = true
	}
	minX, minY, maxX, maxY := viewRect(x, y)
	for ny := minY; ny < maxY; ny++ {
		for nx := minX; nx < maxX; nx++ {
			id := int(g.cell(nx, ny))
			if id <= 0 || id >= MaxPlayer || g.mobs[id].mode == 0 {
				continue
			}
			p := msg
			if own[id] {
				p.Exp, p.Hold = g.mobs[id].info.Player.Exp, int64(g.mobs[id].info.Hold)
			}
			g.queue(g.users[id], &p)
		}
	}
}

// inViewOf reports whether id stands in the view window of (x, y).
func (g *Game) inViewOf(id int, p wire.Position) bool {
	minX, minY, maxX, maxY := viewRect(int(p.X), int(p.Y))
	t := g.mobs[id].target
	return int(t.X) >= minX && int(t.X) < maxX && int(t.Y) >= minY && int(t.Y) < maxY
}

// deleteMob is DeleteMob: tell the view grid, and when reason != 0 free the
// slot (and, for NPCs, the generator count).
func (g *Game) deleteMob(id int, reason int32) {
	m := &g.mobs[id]
	g.multicast2(int(m.target.X), int(m.target.Y),
		&wire.SignalValue{Header: wire.NewHeader(wire.OpSrvRemoveMob, uint16(id)), Value: reason}, 0)
	if reason == 0 {
		return
	}
	if id >= MaxPlayer {
		if m.npc.isSummon {
			g.removeSummon(id)
		}
		g.removeParty(id)
		if gid := m.npc.generateID; gid >= 0 && gid < len(g.genCount) {
			g.genCount[gid] = max(g.genCount[gid]-1, 0)
		}
	}
	m.mode = 0
	g.clearGrid(id)
}

// nnLevelBlocked is _NN_Level_Blocked.
const nnLevelBlocked = 347

// checkGetLevel is CMob::CheckGetLevel: one level per call. Arch 354/369
// and Celestial 39/89 block: the exp is held at the start of the level and
// the character gets no more until Lindy (Arch) or Odin / the 89 item
// (Celestial) unlock it.
// Celestials and Sub-Celestials with the three resets also block at 199
// and 209 (Pedra Ideal / Selo Real unlock), and 199 holds without them.
func (g *Game) checkGetLevel(u *User) {
	if u == nil || u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	ev, level := evolution(pl), int(pl.Status.Level)
	if ev < 0 || ev >= len(nextLevel) || level < 0 || level >= 400 {
		return
	}
	if (ev <= evoArch && level >= maxLevel) || (ev >= evoCelestial && level >= maxLevelCelestial) {
		return
	}
	// Stale Celestial-style blocks are cleared.
	if ev == evoArch && m.info.Info&questLvBlocked != 0 {
		m.info.Info &^= questLvBlocked
	}
	if ev == evoCelestial && m.info.Info&questLvBlocked != 0 && level != 39 && level != 89 {
		m.info.Info &^= questLvBlocked
	}
	if ev >= evoCelestial && level >= 199 && m.info.Info&questUnlock200 == 0 {
		return
	}
	if pl.Exp < nextLevel[ev][level] {
		return
	}
	pl.Status.Level++
	pl.BStatus.Level++
	level++
	hold := func(flag uint64) {
		pl.Exp = nextLevel[ev][level-1]
		m.info.Info |= flag
		g.clientMessage(u, "%s", g.lang(nnLevelBlocked))
		g.sendEtc(u)
	}
	// Mortals and Archs in a guild get its kingdom cape at 220.
	if (ev == evoMortal || ev == evoArch) && level == 219 && pl.GuildIndex != 0 && pl.CapeInfo == 0 {
		if gd := g.guilds[int(pl.GuildIndex)]; gd != nil && (gd.Kingdom == capeBlue || gd.Kingdom == 8) {
			pl.Equip[15] = wire.Item{Index: uint16(545 + gd.Kingdom - capeBlue)}
			g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15]) // the C++ sent it as slot 14
		}
	}
	q := m.info.Info
	block := ev == evoCelestial && ((level == 39 && q&questUnlock39 == 0) || (level == 89 && q&questUnlock89 == 0))
	if ev == evoCelestial || ev == evoSubCele {
		both := q | m.info.Sub.Info
		block = block || (totalResets(q) == 3 &&
			((level == 199 && both&questUnlock200 == 0) || (level == 209 && both&questUnlock210 == 0)))
	}
	switch {
	case block:
		hold(questLvBlocked)
	case level == 199 && totalResets(q) < 3:
		// Without the three resets 199 is the end: the extra exp is lost
		// (the C++ does this for every evolution).
		pl.Exp = nextLevel[ev][level-1]
		g.sendEtc(u)
	}
	// Like the C++, the Arch blocks look at bloqueia354/369, which nothing
	// sets: every level-up into 354 or 369 blocks.
	if ev == evoArch && ((level == 354 && q&questBloqueia354 == 0) || (level == 369 && q&questBloqueia369 == 0)) {
		hold(questLvBlockedArch)
	}
	if pk := int(pl.Inventory[63].Effect[0].Index); pk-75 < 75 {
		pl.Inventory[63].Effect[0].Index = uint8(pk + 3)
		g.sendCreateMob(u, u.id)
	}
	g.getCurrentScore(u.id)
	pl.Status.CurHP, pl.Status.CurMP = pl.Status.MaxHP, pl.Status.MaxMP
	u.potionHP, u.potionMP = pl.Status.MaxHP, pl.Status.MaxMP
	g.sendSetHpMp(u)
	g.sendScore(u.id)
	g.sendEtc(u)
	g.clientMessage(u, "+ + + LEVEL UP + + +")
	g.giveLevelItems(u)
	g.subCythera(u)
}

// removeNPCParty is RemoveParty for a generator group: followers of a dead
// leader go solo; a dead follower leaves its leader's list.
func (g *Game) removeNPCParty(id int) {
	n := &g.mobs[id].npc
	if n.leader == 0 {
		for i, f := range n.followers {
			if f >= MaxPlayer && g.mobs[f].npc.leader == id {
				g.mobs[f].npc.leader = 0
			}
			n.followers[i] = 0
		}
		return
	}
	if l := n.leader; l >= MaxPlayer {
		for i, f := range g.mobs[l].npc.followers {
			if f == id {
				g.mobs[l].npc.followers[i] = 0
			}
		}
	}
	n.leader = 0
}

// playerKilled is the MobKilled branch for a dead player (killed by a mob).
// sServer.DeadPoint is 0 in this server, so mobs take no experience.
// PvP kills go to pvpDeath.
func (g *Game) playerKilled(target, killer int, msg *wire.Killed) {
	dead := &g.mobs[target]
	k := &g.mobs[killer]
	dead.info.Player.Status.CurHP = 0
	msg.Hold, msg.Exp = int64(k.info.Hold), k.info.Player.Exp
	g.familiarLosesRefine(target)
	if killer < MaxPlayer {
		g.pvpDeath(target, killer)
		g.multicastKill(int(dead.target.X), int(dead.target.Y), *msg, g.partyMembers(killer))
		return
	}
	if u := g.player(target); u != nil && dead.info.Player.BStatus.Level <= 399 {
		if g.data.village(int(dead.target.X), int(dead.target.Y)) == 5 {
			if dead.info.Player.BStatus.Level < 20 && evolution(&dead.info.Player) < evoArch {
				g.clientMessage(u, "Level abaixo de 20 não perde experiancia.")
			}
			g.sendEtc(u)
		} else {
			g.clientMessage(u, "Você não pode perder experiancia nesta area")
		}
	}
	g.multicast2(int(dead.target.X), int(dead.target.Y), msg, 0)
	// Mobs forget the dead player.
	for id := MaxPlayer; id < MaxMob; id++ {
		if m := &g.mobs[id]; m.mode == modeBattle {
			m.removeEnemy(target)
			if m.combat.currentTarget == target {
				m.combat.currentTarget = 0
			}
		}
	}
}

// partyBonus is PARTYBONUS[members].
var partyBonus = [13]int64{100, 100, 200, 300, 400, 500, 500, 500, 500, 500, 500, 500, 500}

// inNightmareExp reports the two Nightmare rooms where the C++ skips the
// distance check for party exp.
func inNightmareExp(p wire.Position) bool {
	return (p.X >= 1152 && p.X <= 1282 && p.Y >= 130 && p.Y <= 217) ||
		(p.X >= 1049 && p.X <= 1130 && p.Y >= 272 && p.Y <= 334)
}

// shareExp is the MobKilled exp split: party members within half a view
// window of the killer share base exp weighted by level + 30. With the C++
// constants (PARTYBONUS + g_EmptyMob - 100 for 2+ members) each member's
// share almost always reaches the cap: the base exp, +30.
//
// Changed from the C++ (by request): the base exp is scaled by each
// member's own level (baseFor(id) = GetExpApply for that member) instead
// of the killer's, so a capped or much stronger killer no longer leaves the
// rest of the party with only the +30.
func (g *Game) shareExp(baseFor func(id int) int64, killer int, at wire.Position) {
	const bonus = 30
	kpos := g.mobs[killer].target
	near := func(id int) bool {
		p := g.mobs[id].target
		return int(kpos.X) >= int(p.X)-halfGrid && int(kpos.X) <= int(p.X)+halfGrid &&
			int(kpos.Y) >= int(p.Y)-halfGrid && int(kpos.Y) <= int(p.Y)+halfGrid
	}
	members := g.partyMembers(killer)
	var count, levels int64
	for _, id := range members {
		if near(id) {
			levels += int64(g.mobs[id].info.Player.Status.Level) + bonus
			count++
		}
	}
	if count <= 0 || count > 13 || levels <= 0 {
		return // the C++ sent "Error 1" here
	}
	mult := partyBonus[count]
	if count > 1 {
		mult += MaxPlayer - 100 // static g_EmptyMob = MAX_PLAYER
	}
	for _, id := range members {
		m := &g.mobs[id]
		u := g.player(id)
		if u == nil || u.status != userPlay || m.info.Player.Status.CurHP == 0 {
			continue
		}
		if !inNightmareExp(m.target) && !near(id) {
			continue
		}
		base := baseFor(id)
		total := base * mult / 25
		exp := (int64(m.info.Player.Status.Level) + bonus) * total / levels
		if exp < 0 || exp > 500000000 {
			exp = 500000000
		}
		exp = min(exp*6/10, base) + bonus
		exp = g.expApply2(exp, id, killer, true)
		if hold := m.info.Hold; hold > 0 {
			if hold > exp {
				m.info.Hold -= exp
				exp = 0
			} else {
				exp -= hold
				m.info.Hold = 0
			}
		}
		g.checkQuarter(u, exp)
		m.info.Player.Exp += exp
		g.checkGetLevel(u)
		// Members in view get their exp in the kill packet (which shows
		// "Exp +N"); 0x337 would set it silently first, so only those out
		// of view get it.
		if id != killer && !g.inViewOf(id, at) {
			g.sendEtc(u)
		}
	}
}

// specialMobKilled runs the deaths with a rule of their own. True when the
// mob stays in the world.
func (g *Game) specialMobKilled(target, killer int) bool {
	if target < MaxPlayer {
		return false
	}
	g.bossKilled(target, killer)
	gen := g.mobs[target].npc.generateID
	g.pistaMobKilled(gen, killer)
	switch {
	case gen == genTowerErion:
		g.erionTowerKilled(target, killer)
		return true
	case gen == genRvRTowerBlue || gen == genRvRTowerRed:
		g.rvrTowerKilled(target, killer, gen)
		return true
	case gen == genKingBlue || gen == genKingRed:
		return g.kingKilled(target, killer, gen)
	case gen == genTowerGuardB || gen == genTowerGuardR:
		g.kingdomTowerKilled(gen)
	case gen == genKefra:
		g.kefraKilled(target, killer)
	case gen == genZakum:
		g.zakumKilled(killer)
	case gen >= genColiseum && gen < genColiseum+4:
		g.coliseumBossKilled(target)
	}
	return false
}
