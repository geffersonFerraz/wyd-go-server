package game

import (
	"fmt"
	"slices"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Kefra, the kingdom battle, the RvR, the Coliseum, Pesadelo and the water
// rooms (Basedef.cpp, Water.cpp, ProcessTimer.cpp, RequestNightmareAccept.cpp).

const (
	genKefra        = 155
	genKefraGuard   = 156
	kefraGuards     = 18
	genRvRTowerBlue = 288
	genRvRTowerRed  = 289
	genRvRStoreBlue = 384
	genRvRStoreRed  = 385
	genTowerGuardB  = 382
	genTowerGuardR  = 383
	genKingBlue     = 8
	genKingRed      = 9
	genColiseum     = 84
	genColiseumTyr  = 124
	coliseumSeconds = 300

	opSrvRealBattle   = 0x637
	opSrvNightmareOK  = 0x630
	opSrvQuestSeconds = 0x3A1
	opSrvNightmareOn  = 0x3A7
	opSrvMobsLeft     = 0x3B0
	opCliNightmare    = 0x631
	opCliChallenge    = 0x28E
	opCliDeclareWar   = 0xED7

	langKefraGuild  = 398
	langKefraPlayer = 399
	langKefraReborn = 400
	langOnlyErion   = 386
	langOnlyArmia   = 385
	langOnlyArzan   = 360
	langPartyLeader = 229
	langCantEnter   = 361
	langLevelRange  = 313
	langNoEntries   = 357
	langNoRoom      = 359
	langOnlyKingdom = 282
)

var (
	rvrArea     = mapRect{1041, 1950, 1248, 2158}
	rvrBases    = []mapRect{{1230, 1949, 1244, 1987}, {1205, 1949, 1244, 1962}, {1056, 2108, 1068, 2146}, {1056, 2132, 1095, 2142}}
	kefraArea   = mapRect{3200, 1664, 3328, 1791}
	coliseumIn  = mapRect{2604, 1708, 2650, 1748}
	coliseumOut = mapRect{2608, 1708, 2647, 1748}
)

// Kefra -----------------------------------------------------------------

type kefraState struct {
	dead   bool
	killer int32
}

func (g *Game) kefraKilled(target, killer int) {
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	k := &g.kefra
	k.killer = 0
	guild := 0
	if killer < MaxPlayer && g.users[killer].status == userPlay {
		guild = int(g.mobs[killer].info.Player.GuildIndex)
		if guild != 0 {
			g.setGuildFame(guild, g.guild(guild).Fame+100)
			k.killer = int32(guild)
		}
		var p wire.KefraDeath
		p.Header = wire.NewHeader(wire.OpNotifyKefraDeath, 0)
		copy(p.Name[:], wire.CString(g.mobs[killer].info.Player.Name[:]))
		g.sendDB(&p)
	}
	for i := genKefraGuard; i < genKefraGuard+kefraGuards && i < len(g.data.Geners); i++ {
		g.data.Geners[i].MinuteGenerate = -1
	}
	for id := firstNPC; id < MaxMob; id++ {
		gen := g.mobs[id].npc.generateID
		if id != target && g.mobs[id].mode != 0 && gen >= genKefraGuard && gen <= genKefraGuard+kefraGuards {
			g.deleteMob(id, 1)
		}
	}
	k.dead = true
	if k.killer != 0 {
		g.notice(fmt.Sprintf("O jogador [%s] da guild [%s] derrotou Kefra", g.charName(killer), g.guildName(guild)))
	} else {
		g.notice(g.lang(langKefraPlayer))
	}
	g.log.Info("kefra killed", "killer", killer, "guild", guild)
}

// rebornKefra is RebornKefra: DBSRV orders it every Wednesday.
func (g *Game) rebornKefra() {
	if !g.kefra.dead {
		return
	}
	for i := genKefraGuard; i < genKefraGuard+kefraGuards && i < len(g.data.Geners); i++ {
		g.data.Geners[i].MinuteGenerate = 4
		g.generateMob(i, 0, 0)
	}
	g.kefra = kefraState{}
	g.generateMob(genKefra, 0, 0)
	g.notice(g.lang(langKefraReborn))
	g.clearArea(kefraArea)
	g.log.Info("kefra reborn")
}

// Kingdom battle --------------------------------------------------------

type kingdomInfo struct {
	towerID, kingID       int
	towerAlive, kingAlive bool
	status                bool
}

type kingdomState struct {
	winner int32
	info   [2]kingdomInfo
}

func (g *Game) sendKingdomInfo(to *User, kingdom int, status bool) {
	v := int32(0)
	if status {
		v = 1
	}
	p := &wire.Parm2{Header: wire.NewHeader(opSrvRealBattle, wire.ClientIDSystem), Parm1: int32(kingdom), Parm2: v}
	if to != nil {
		g.queue(to, p)
		return
	}
	g.forEachPlaying(func(u *User) { g.queue(u, p) })
}

func (g *Game) kingdomLogin(u *User) {
	k := &g.kingdom
	if k.winner == capeBlue || k.winner == capeRed {
		name := "Blue"
		if k.winner == capeRed {
			name = "Red"
		}
		g.clientMessage(u, "O Reino %s é o atual dominante", name)
	}
	g.sendKingdomInfo(u, capeBlue, k.info[0].status)
	g.sendKingdomInfo(u, capeRed, k.info[1].status)
}

// kingdomCanSpawn: the tower does not come back while its king is fighting.
func (g *Game) kingdomCanSpawn(gen int) bool {
	if gen != genTowerGuardB && gen != genTowerGuardR {
		return true
	}
	i := gen - genTowerGuardB
	king := g.kingdom.info[i].kingID
	return king == 0 || g.mobs[king].npc.generateID != genKingBlue+i || g.mobs[king].mode != modeBattle
}

func (g *Game) kingdomSpawned(gen, id int) {
	k := &g.kingdom
	switch gen {
	case genTowerGuardB, genTowerGuardR:
		i := gen - genTowerGuardB
		k.info[i].towerID, k.info[i].towerAlive, k.info[i].status = id, true, false
	case genKingBlue, genKingRed:
		i := gen - genKingBlue
		k.info[i].kingID, k.info[i].kingAlive = id, true
		if t := k.info[i].towerID; t == 0 || g.mobs[t].npc.generateID != genTowerGuardB+i {
			g.generateMob(genTowerGuardB+i, 0, 0)
			k.info[i].status = false
		}
	}
}

// kingdomHit is the start of a fight with a tower or a king (MobKilled mode != 5).
func (g *Game) kingdomHit(gen int) {
	k := &g.kingdom
	notice := map[int]string{
		genTowerGuardB: "A Torre Guardião do reino Blue está sendo atacada",
		genTowerGuardR: "A Torre Guardião do reino Red está sendo atacada",
		genKingBlue:    "O Rei Harabard está sendo atacado",
		genKingRed:     "O Rei Glantuar está sendo atacado",
	}[gen]
	if notice == "" {
		return
	}
	i, cape := 0, capeBlue
	if gen == genTowerGuardR || gen == genKingRed {
		i, cape = 1, capeRed
	}
	g.notice(notice)
	g.sendKingdomInfo(nil, cape, true)
	k.info[i].status = true
}

// kingdomPeace: a tower or king went back to peace (ProcessTimer).
func (g *Game) kingdomPeace(gen int) {
	var i, other int
	k := &g.kingdom
	var alive bool
	switch gen {
	case genTowerGuardB, genTowerGuardR:
		i = gen - genTowerGuardB
		other, alive = k.info[i].kingID, k.info[i].kingAlive
		if other != 0 && g.mobs[other].npc.generateID != genKingBlue+i {
			alive = false
		}
	case genKingBlue, genKingRed:
		i = gen - genKingBlue
		other, alive = k.info[i].towerID, k.info[i].towerAlive
		if other != 0 && g.mobs[other].npc.generateID != genTowerGuardB+i {
			alive = false
		}
	default:
		return
	}
	status := alive && other > 0 && g.mobs[other].mode == modeBattle
	g.sendKingdomInfo(nil, capeBlue+i, status)
	k.info[i].status = status
	if !status {
		name := "Blue"
		if i == 1 {
			name = "Red"
		}
		g.notice("A paz situou no Reino " + name)
	}
}

// kingKilled: a king dies only after its tower. True keeps the king alive.
func (g *Game) kingKilled(target, killer, gen int) bool {
	k := &g.kingdom
	i := gen - genKingBlue
	if k.info[i].towerAlive {
		if ku := g.player(killer); ku != nil {
			g.clientMessage(ku, "Para derrotar o Rei, é necessário passar pela Torre Guardiã!")
		}
		g.mobs[target].info.Player.Status.CurHP = g.mobs[target].info.Player.Status.MaxHP
		g.sendGridMob(target)
		return true
	}
	winner, loser, cape := "Red", "Blue", capeRed
	if gen == genKingRed {
		winner, loser, cape = "Blue", "Red", capeBlue
	}
	k.winner = int32(cape)
	g.notice(fmt.Sprintf("O Reino %s conquistou o reino %s", winner, loser))
	k.info[i].status = false
	k.info[i].kingAlive = false
	g.forEachPlaying(func(u *User) {
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
	})
	g.sendKingdomInfo(nil, capeBlue, k.info[0].status)
	g.sendKingdomInfo(nil, capeRed, k.info[1].status)
	return false
}

// kingdomTowerKilled records a tower's fall.
func (g *Game) kingdomTowerKilled(gen int) {
	i := gen - genTowerGuardB
	name := "Blue"
	if i == 1 {
		name = "Red"
	}
	g.notice(fmt.Sprintf("A Torre Guardiã do Reino %s foi derrotada", name))
	g.kingdom.info[i].towerAlive, g.kingdom.info[i].towerID = false, 0
}

// kingdomRegen is the +2 regen of the winning kingdom and of the RvR's.
func (g *Game) kingdomRegen(cape int) int {
	bonus := 0
	if g.rvr.bonus != 0 && int(g.rvr.bonus) == cape {
		bonus += 2
	}
	if g.kingdom.winner != 0 && int(g.kingdom.winner) == cape {
		bonus += 2
	}
	return bonus
}

// RvR ---------------------------------------------------------------------

type rvrState struct {
	on       bool
	warned   bool
	scored   bool
	points   [2]int
	bonus    int32
	storeOut bool
}

// rvrBase picks the respawn point of a kingdom.
func rvrBase(cape int) (int, int) {
	if cape == capeBlue {
		if rnd()%2 == 0 {
			return 1061 - rnd()%5, 2113 + rnd()%5
		}
		return 1091 - rnd()%5, 2140 + rnd()%5
	}
	if rnd()%2 == 0 {
		return 1238 + rnd()%5, 1983 + rnd()%5
	}
	return 1211 + rnd()%5, 1955 + rnd()%5
}

func (g *Game) inRvR(m *Mob) bool {
	return g.rvr.on && rvrArea.contains(int(m.target.X), int(m.target.Y))
}

// rvrRecall: during the RvR a fighter respawns at its base (DoRecall).
func (g *Game) rvrRecall(id int) (int, int, bool) {
	m := &g.mobs[id]
	cape := int(m.info.Player.CapeInfo)
	if !g.inRvR(m) || (cape != capeBlue && cape != capeRed) {
		return 0, 0, false
	}
	x, y := rvrBase(cape)
	return x, y, true
}

func (g *Game) sendCounterRvR(u *User) {
	g.queue(u, &wire.Parm1{Header: wire.NewHeader(opSrvCounterMob, uint16(u.id)), Parm1: int32(g.rvr.points[1]) | int32(g.rvr.points[0])<<16})
}

func (g *Game) forEachInRvR(fn func(u *User)) {
	g.forEachPlaying(func(u *User) {
		m := &g.mobs[u.id]
		if rvrArea.contains(int(m.target.X), int(m.target.Y)) {
			fn(u)
		}
	})
}

// rvrTick: the live C++ code starts the war at 20:00 and never ends it. The
// end here is the block commented out in ProcessTimer.cpp.
func (g *Game) rvrTick(now time.Time) {
	r := &g.rvr
	if r.on {
		g.rvrHeal()
	}
	if now.Weekday() == time.Sunday {
		return
	}
	h, minute := g.cfg.Wars.RvRHour, now.Minute()
	switch {
	case now.Hour() == h-1 && minute == 55 && !r.warned:
		g.notice("A guerra de reinos iniciará em 5 minutos. Acesse o teleporte de Noatun.")
		r.warned = true
	case now.Hour() == h && minute == 0 && !r.on:
		g.generateMob(genRvRTowerBlue, 0, 0)
		g.generateMob(genRvRTowerRed, 0, 0)
		r.on, r.points = true, [2]int{}
		g.notice("Guerra RvR foi iniciada!")
		g.log.Info("rvr started")
	case now.Hour() == h && minute < 30 && minute%2 == 0 && !r.scored && r.on:
		g.notice(fmt.Sprintf("Guerra Reino vs Reino - Red: %d - Blue: %d", r.points[1], r.points[0]))
		r.scored = true
	case now.Hour() == h && minute%2 == 1 && r.scored:
		r.scored = false
	case now.Hour() == h && minute == 30 && r.on:
		g.finishRvR()
	case now.Hour() == h && minute == 35 && r.storeOut:
		for id := firstNPC; id < MaxMob; id++ {
			if gen := g.mobs[id].npc.generateID; g.mobs[id].mode != 0 && (gen == genRvRStoreBlue || gen == genRvRStoreRed) {
				g.deleteMob(id, 1)
			}
		}
		r.storeOut = false
	}
}

// rvrHeal is the 5% a second at the bases.
func (g *Game) rvrHeal() {
	g.forEachPlaying(func(u *User) {
		m := &g.mobs[u.id]
		s := &m.info.Player.Status
		if (m.info.Player.CapeInfo != capeBlue && m.info.Player.CapeInfo != capeRed) || s.CurHP >= s.MaxHP || s.CurHP <= 0 {
			return
		}
		x, y := int(m.target.X), int(m.target.Y)
		if !slices.ContainsFunc(rvrBases, func(r mapRect) bool { return r.contains(x, y) }) {
			return
		}
		heal := s.MaxHP * 5 / 100
		s.CurHP = min(s.CurHP+heal, s.MaxHP)
		u.potionHP += heal
		g.setReqHp(u)
		g.setReqMp(u)
		g.sendSetHpMp(u)
	})
}

func (g *Game) finishRvR() {
	r := &g.rvr
	for id := firstNPC; id < MaxMob; id++ {
		if gen := g.mobs[id].npc.generateID; g.mobs[id].mode != 0 && (gen == genRvRTowerBlue || gen == genRvRTowerRed) {
			g.deleteMob(id, 1)
		}
	}
	winner := 0
	switch {
	case r.points[0] > r.points[1]:
		winner = capeBlue
		g.notice(fmt.Sprintf("O reino azul foi o vencedor do RvR com %d pontos vs %d!", r.points[0], r.points[1]))
		g.generateMob(genRvRStoreBlue, 0, 0)
	case r.points[1] > r.points[0]:
		winner = capeRed
		g.notice(fmt.Sprintf("O reino vermelho foi o vencedor do RvR com %d pontos vs %d!", r.points[1], r.points[0]))
		g.generateMob(genRvRStoreRed, 0, 0)
	default:
		g.notice("A guerra do RvR terminou em empate!")
	}
	r.storeOut = winner != 0
	r.bonus = int32(winner)
	r.on, r.warned = false, false
	g.log.Info("rvr finished", "winner", winner, "blue", r.points[0], "red", r.points[1])
	r.points = [2]int{}
	g.forEachInRvR(func(u *User) {
		cape := int(g.mobs[u.id].info.Player.CapeInfo)
		if cape != winner {
			g.doRecall(u.id)
			return
		}
		x, y := rvrBase(cape)
		g.teleportar(u.id, x, y)
	})
	if winner != 0 {
		g.forEachPlaying(func(u *User) {
			if int(g.mobs[u.id].info.Player.CapeInfo) == winner {
				g.getCurrentScore(u.id)
				g.sendScore(u.id)
			}
		})
	}
}

// rvrHitBlocked: nobody hurts its own kingdom or its own tower in the RvR.
func (g *Game) rvrHitBlocked(attacker, target int) bool {
	if !g.rvr.on || attacker >= MaxPlayer {
		return false
	}
	a, t := &g.mobs[attacker].info.Player, &g.mobs[target]
	if target >= MaxPlayer {
		gen := t.npc.generateID
		if (gen == genRvRTowerBlue && a.CapeInfo == capeBlue) || (gen == genRvRTowerRed && a.CapeInfo == capeRed) {
			return true
		}
		if t.info.Player.CapeInfo == 4 {
			s := t.npc.summoner
			return s != 0 && g.mobs[s].info.Player.CapeInfo == a.CapeInfo
		}
		return false
	}
	return rvrArea.contains(int(t.target.X), int(t.target.Y)) && t.info.Player.CapeInfo == a.CapeInfo
}

// rvrTowerKilled: the tower stands up, 25 points for the other kingdom.
func (g *Game) rvrTowerKilled(target, killer, gen int) {
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	m := &g.mobs[target]
	m.info.Player.Status.CurHP = m.info.Player.Status.MaxHP
	g.sendGridMob(target)
	side, towerName, sideName := 0, "Red", "Blue"
	if gen == genRvRTowerBlue {
		side, towerName, sideName = 1, "Blue", "Red"
	}
	g.rvr.points[side] += 25
	name := ""
	if killer < MaxPlayer && g.users[killer].status == userPlay {
		g.mobs[killer].info.RvRPoints += 15
		name = g.charName(killer)
	}
	g.forEachInRvR(func(u *User) {
		g.clientMessage(u, "A torre do reino %s foi destruída por %s. +25 pontos para o reino %s.", towerName, name, sideName)
		if g.rvr.on {
			x, y := rvrBase(int(g.mobs[u.id].info.Player.CapeInfo))
			g.teleportar(u.id, x, y)
		}
		g.sendCounterRvR(u)
	})
}

// rvrKill scores a kill between kingdoms.
func (g *Game) rvrKill(dead, killer int) {
	if killer >= MaxPlayer || !g.inRvR(&g.mobs[dead]) {
		return
	}
	d, k := int(g.mobs[dead].info.Player.CapeInfo), int(g.mobs[killer].info.Player.CapeInfo)
	if (d != capeBlue && d != capeRed) || (k != capeBlue && k != capeRed) || d == k {
		return
	}
	g.rvr.points[k-capeBlue]++
	g.mobs[killer].info.RvRPoints++
	team := "Blue"
	if k == capeRed {
		team = "Red"
	}
	g.forEachInRvR(func(u *User) {
		g.clientMessage(u, "%s matou %s e pontuou para o reino %s", g.charName(killer), g.charName(dead), team)
		g.sendCounterRvR(u)
	})
}

// rvrWallBlocked: each kingdom's gate only opens to its own side.
func (g *Game) rvrWallBlocked(cape, x, y, curY int) bool {
	if !g.rvr.on {
		return false
	}
	if x >= 1710 && x <= 1715 && y >= 1969 && curY <= y && cape != capeRed {
		return true
	}
	return x >= 1748 && x <= 1754 && y >= 1969 && y <= 1994 && cape != capeBlue
}

// rvrPortal is teleport 6.
func (g *Game) rvrPortal(u *User) {
	if !g.rvr.on {
		g.clientMessage(u, "Use as %d:00hrs de segunda a sábado para ir na RVR.", g.cfg.Wars.RvRHour)
		g.teleportar(u.id, 1178, 1722)
		return
	}
	cape := int(g.mobs[u.id].info.Player.CapeInfo)
	if cape != capeBlue && cape != capeRed {
		g.clientMessage(u, "%s", g.lang(langOnlyKingdom))
		return
	}
	x, y := rvrBase(cape)
	g.teleportar(u.id, x, y)
	g.sendCounterRvR(u)
}

// Coliseum ----------------------------------------------------------------

type coliseumState struct {
	mystic      bool
	level       int
	seconds     int
	boss        bool
	born        bool
	closedGate  bool
	closedWalls bool
	clients     []int
}

func (g *Game) genAlive(gen int) int {
	if gen < 0 || gen >= len(g.genCount) {
		return 0
	}
	return g.genCount[gen]
}

func (g *Game) coliseumNotify(msg string, seconds int) {
	g.forEachPlaying(func(u *User) {
		m := &g.mobs[u.id]
		if !coliseumOut.contains(int(m.target.X), int(m.target.Y)) {
			return
		}
		if msg != "" {
			g.clientMessage(u, "%s", msg)
		}
		if seconds > 0 {
			g.signalParm(u, wire.ClientIDSystem, opSrvQuestSeconds, int32(seconds))
		}
	})
}

func (g *Game) coliseumTick(now time.Time) {
	c := &g.coliseum
	g.coliseumRun()
	wd, h, minute := now.Weekday(), now.Hour(), now.Minute()
	if wd == time.Sunday || wd == time.Saturday || (h != 20 && h != 15) {
		return
	}
	switch {
	case (minute == 0 || minute == 30) && !c.closedGate:
		c.mystic = minute == 30
		c.closedGate = true
		g.setDoors(13, 2, gateOpen, true)
	case (minute == 3 || minute == 33) && !c.closedWalls:
		g.startColiseum()
	case c.level != 0:
		g.forEachPlaying(func(u *User) {
			m := &g.mobs[u.id]
			if coliseumOut.contains(int(m.target.X), int(m.target.Y)) && !slices.Contains(c.clients, u.id) {
				g.doRecall(u.id)
			}
		})
	}
}

func (g *Game) startColiseum() {
	c := &g.coliseum
	c.closedWalls, c.level, c.seconds, c.born = true, 1, coliseumSeconds, true
	g.setDoors(13, 2, gateLocked, true)
	g.setDoors(15, 5, gateOpen, true)
	for i := range 9 {
		g.generateMob(genColiseum+4+i, 0, 0)
	}
	g.forEachPlaying(func(u *User) {
		m := &g.mobs[u.id]
		if !coliseumIn.contains(int(m.target.X), int(m.target.Y)) {
			return
		}
		ev := evolution(&m.info.Player)
		if (!c.mystic && ev != evoMortal) || (c.mystic && ev < evoArch) {
			g.doRecall(u.id)
			return
		}
		if slices.ContainsFunc(c.clients, func(id int) bool { return g.users[id].mac == u.mac }) {
			g.clientMessage(u, "Somente uma conta por computador")
			return
		}
		g.signalParm(u, wire.ClientIDSystem, opSrvQuestSeconds, int32(c.seconds))
		c.clients = append(c.clients, u.id)
	})
}

func (g *Game) finishColiseum() {
	*(&g.coliseum) = coliseumState{}
	g.setDoors(13, 2, gateOpen, true)
	g.setDoors(15, 5, gateLocked, true)
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && coliseumOut.contains(int(m.target.X), int(m.target.Y)) {
			g.deleteMob(id, 1)
		}
	}
	g.clearArea(coliseumOut)
}

// coliseumRun is DoColosseum.
func (g *Game) coliseumRun() {
	c := &g.coliseum
	if c.level == 0 || !c.closedWalls {
		return
	}
	c.seconds--
	if c.seconds <= 0 {
		g.finishColiseum()
		return
	}
	base := genColiseum + 4 + 9*(c.level-1)
	alive := g.genAlive(genColiseum + c.level - 1)
	if !c.boss {
		alive = 0
		for i := range 9 {
			alive += g.genAlive(base + i)
		}
	}
	if c.level == 4 && g.genAlive(genColiseumTyr) == 0 {
		g.finishColiseum()
		return
	}
	if alive != 0 {
		return
	}
	switch {
	case !c.boss:
		c.seconds, c.boss = coliseumSeconds, true
		g.coliseumNotify("Boss nasceu!", 0)
		g.generateMob(genColiseum+c.level-1, 0, 0)
	case c.level == 4:
		g.finishColiseum()
	default:
		msg := fmt.Sprintf("Boss derrotado. Nível %d alcançado...", c.level+1)
		if c.level == 3 {
			msg = "Proteja Tyr para continuar sua missão! Caso ele morra, o Coliseu acaba!"
		}
		c.seconds, c.boss = coliseumSeconds, false
		c.level++
		g.coliseumNotify(msg, c.seconds)
		next := genColiseum + 4 + (c.level-1)*9
		for i := range 9 {
			g.generateMob(next+i, 0, 0)
		}
		if c.level == 4 {
			g.generateMob(genColiseumTyr, 0, 0)
		}
	}
}

// coliseumBossKilled: everyone inside has a chance of each item the boss
// carries (the C++ gave 40%, or 75% in the mystic Coliseum).
func (g *Game) coliseumBossKilled(target int) {
	rate := 40
	if g.coliseum.mystic {
		rate = 75
	}
	drop := g.mobDrops(&g.mobs[target].info.Player)
	if drop == nil {
		return
	}
	g.forEachPlaying(func(u *User) {
		m := &g.mobs[u.id]
		if m.info.Player.Status.CurHP <= 0 || !coliseumIn.contains(int(m.target.X), int(m.target.Y)) {
			return
		}
		for _, r := range drop.Rate {
			if it := r.Item; rnd()%100 < rate {
				g.eventItem(u, &it, "Coliseu: "+mobName(&g.mobs[target].info.Player))
			}
		}
	})
}

// Pesadelo ----------------------------------------------------------------

const (
	nightmareMembers = 40
	nightmareN       = 0
	nightmareM       = 1
	nightmareA       = 2
	maxPesaLevel     = 120
)

var (
	nightmareArea = [3]mapRect{{}, {1024, 257, 1151, 384}, {1153, 132, 1279, 254}}
	// nightmareSpawns: the first of each row is the room's boss group.
	nightmareSpawns = [3][8][3]int{
		{{291, 296, 301}, {292, 297, 302}, {293, 298, 303}, {294, 299, 305}, {295, 300}},
		{{266, 274, 275}, {267, 282, 283}, {268, 284, 280}, {269, 285, 279}, {270, 281}, {271, 275, 276}, {272, 286, 287}, {273, 278}},
		{{246, 254, 265}, {247, 255}, {248, 256}, {249, 257}, {250, 258}, {251, 259, 264}, {252, 260, 263}, {253, 261, 262}},
	}
	nightmareCoordsM = [6][2]int{{1116, 328}, {1089, 333}, {1064, 333}, {1066, 307}, {1077, 323}, {1088, 290}}
)

type nightmareRoom struct {
	status   int // 0 off, 1 waiting 4 minutes, 2 running
	timeLeft int
	alive    [8]bool // true: the group was killed
	members  [nightmareMembers]int
	names    [nightmareMembers]string
}

// nightmareStart: A at :10/:30/:50, M at :05/:25/:45. The N room is off in
// the C++ (it sets its status to 0) and its area is empty, so it stays off.
func (g *Game) nightmareStart(now time.Time) {
	if now.Minute() == g.nightmareLastMin {
		return
	}
	g.nightmareLastMin = now.Minute()
	start := func(i int) {
		g.clearArea(nightmareArea[i])
		for x := range 8 {
			g.generateMob(nightmareSpawns[i][x][0], 0, 0)
		}
		g.nightmare[i] = nightmareRoom{status: 1, timeLeft: 240}
	}
	switch now.Minute() {
	case 5, 25, 45:
		start(nightmareM)
	case 10, 30, 50:
		start(nightmareA)
	}
}

// nightmareTimer is DoNightmare, every 2 seconds.
func (g *Game) nightmareTimer() {
	for i := nightmareM; i <= nightmareA; i++ {
		n := &g.nightmare[i]
		if n.status == 0 {
			continue
		}
		timer := n.timeLeft - 2
		if timer <= 0 && n.status == 1 {
			for x := range 8 {
				for y := 1; y < 3; y++ {
					if s := nightmareSpawns[i][x][y]; s > 0 {
						g.generateMob(s, 0, 0)
					}
				}
			}
			g.forNightmareMembers(n, func(u *User, _ int) {
				g.signalParm(u, uint16(u.id), opSrvMobsLeft, 8)
				g.signalParm(u, uint16(u.id), opSrvQuestSeconds, 15*60)
			})
			n.status, timer = 2, 15*60
		}
		if n.status == 2 {
			alive := 0
			for x := range 8 {
				if n.alive[x] {
					continue
				}
				boss := nightmareSpawns[i][x][0]
				if boss <= 0 {
					continue
				}
				if g.genAlive(boss) == 0 {
					n.alive[x] = true
					continue
				}
				alive++
				for y := 1; y < 3; y++ {
					if s := nightmareSpawns[i][x][y]; s > 0 {
						g.generateMob(s, 0, 0)
					}
				}
			}
			g.forNightmareMembers(n, func(u *User, slot int) {
				g.signalParm(u, uint16(u.id), opSrvMobsLeft, int32(alive))
				pl := &g.mobs[u.id].info.Player
				if evolution(pl) >= evoCelestial && int(pl.BStatus.Level) >= maxPesaLevel {
					g.doRecall(u.id)
					n.members[slot], n.names[slot] = 0, ""
				}
			})
		}
		if timer <= 0 {
			for id := firstNPC; id < MaxMob; id++ {
				m := &g.mobs[id]
				if m.mode != 0 && nightmareArea[i].contains(int(m.target.X), int(m.target.Y)) {
					g.deleteMob(id, 1)
				}
			}
			g.clearArea(nightmareArea[i])
			*n = nightmareRoom{}
			continue
		}
		n.timeLeft = timer
	}
}

func (g *Game) forNightmareMembers(n *nightmareRoom, fn func(u *User, slot int)) {
	for slot, id := range n.members {
		if id <= 0 || id >= MaxPlayer {
			continue
		}
		if g.users[id].status != userPlay {
			n.members[slot] = 0
			continue
		}
		fn(g.users[id], slot)
	}
}

// nightmareDeadBosses is how many boss groups are still alive, for the exp.
func (g *Game) nightmareExp(exp int64, attacker int) int64 {
	m := &g.mobs[attacker]
	x, y := int(m.target.X), int(m.target.Y)
	room := -1
	switch {
	case x >= 1152 && x <= 1282 && y >= 130 && y <= 217:
		room = nightmareA
	case x >= 1049 && x <= 1130 && y >= 272 && y <= 334:
		room = nightmareM
	}
	if room < 0 {
		return exp
	}
	switch evolution(&m.info.Player) {
	case evoMortal:
		exp += exp * 20 / 100
	case evoArch:
		exp += exp * 15 / 100
	}
	killed := 0
	for _, dead := range g.nightmare[room].alive {
		if dead {
			killed++
		}
	}
	if killed != 0 {
		exp -= exp * int64(killed) * 10 / 100
	}
	return exp
}

func nightmarePos(room int, info *wire.CharInfo) (int, int) {
	if p := info.Nightmare[room]; p.X != 0 && p.Y != 0 {
		return int(p.X), int(p.Y)
	}
	if room == nightmareM {
		c := nightmareCoordsM[rnd()%len(nightmareCoordsM)]
		return c[0], c[1]
	}
	return 1210, 180
}

// useNightmareScroll is the region PESADELOS: volatile 173, 174 and 175.
func (g *Game) useNightmareScroll(u *User, p *wire.UseItem, src *wire.Item, vol int) bool {
	if vol < 173 || vol > 175 {
		return false
	}
	refuse := func(msg string) bool {
		g.clientMessage(u, "%s", msg)
		g.sendItem(u, wire.SlotInv, int(p.SrcSlot), src)
		return true
	}
	room := vol - 173
	m := &g.mobs[u.id]
	village := [3]int{2, 0, 1}[room]
	onlyHere := [3]int{langOnlyErion, langOnlyArmia, langOnlyArzan}[room]
	if g.data.village(int(m.target.X), int(m.target.Y)) != village {
		return refuse(g.lang(onlyHere))
	}
	if m.npc.leader != 0 {
		return refuse(g.lang(langPartyLeader))
	}
	n := &g.nightmare[room]
	if n.status != 1 || n.timeLeft <= 0 {
		return refuse(g.lang(langCantEnter))
	}
	var players []int
	for _, id := range append(slices.Clone(g.partyMembers(u.id)), u.id) {
		if id <= 0 || id >= MaxPlayer || slices.Contains(players, id) || g.users[id].status != userPlay {
			continue
		}
		pl := &g.mobs[id].info
		if evolution(&pl.Player) >= evoCelestial {
			if pl.PesaEnter <= 0 {
				g.clientMessage(g.users[id], "%s", g.lang(langNoEntries))
				continue
			}
			if int(pl.Player.Status.Level) >= maxPesaLevel {
				g.clientMessage(g.users[id], g.lang(langLevelRange), 1, maxPesaLevel)
				continue
			}
		}
		players = append(players, id)
	}
	free := 0
	for _, id := range n.members {
		if id == 0 {
			free++
		}
	}
	if len(players) > free {
		return refuse(g.lang(langNoRoom))
	}
	if len(players) == 0 {
		return refuse("Ninguém no grupo está habilitado a entrar no Pesadelo")
	}
	for _, id := range players {
		slot := slices.Index(n.members[:], 0)
		info := &g.mobs[id].info
		if evolution(&info.Player) >= evoCelestial {
			info.PesaEnter--
		}
		n.members[slot], n.names[slot] = id, g.charName(id)
		x, y := nightmarePos(room, info)
		g.teleportar(id, x, y)
		g.signalParm(g.users[id], wire.ClientIDSystem, opSrvNightmareOn, 2)
		g.signalParm(g.users[id], uint16(id), opSrvQuestSeconds, int32(n.timeLeft))
	}
	amountMinus(src)
	g.sendItem(u, wire.SlotInv, int(p.SrcSlot), src)
	return true
}

// requestNightmareAccept is 0x631: back into a room after a disconnection.
func (g *Game) requestNightmareAccept(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if u.status != userPlay || wire.Unmarshal(pkt.Raw, &p) != nil || p.Value < 0 || p.Value >= 3 {
		return
	}
	room := int(p.Value)
	n := &g.nightmare[room]
	name := g.charName(u.id)
	slot := slices.Index(n.names[:], name)
	if slot < 0 || name == "" {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if evolution(pl) >= evoCelestial && int(pl.BStatus.Level) >= maxPesaLevel {
		g.clientMessage(u, "Limite de nível atingido. Não é possível entrar na área do Pesadelo")
		return
	}
	x, y := nightmarePos(room, &g.mobs[u.id].info)
	n.members[slot] = u.id
	g.teleportar(u.id, x, y)
	g.signalParm(u, wire.ClientIDSystem, opSrvNightmareOn, 2)
	g.signalParm(u, uint16(u.id), opSrvQuestSeconds, int32(n.timeLeft))
}

func (g *Game) nightmareLogin(u *User) {
	name := g.charName(u.id)
	for i := range g.nightmare {
		if name != "" && slices.Contains(g.nightmare[i].names[:], name) {
			g.queue(u, &wire.Parm2{Header: wire.NewHeader(opSrvNightmareOK, wire.ClientIDSystem), Parm1: int32(i), Parm2: int32(g.nightmare[i].timeLeft)})
		}
	}
}

// saveNightmareWarp is the star gem inside a Nightmare room.
func (g *Game) saveNightmareWarp(u *User, x, y int) bool {
	for i, a := range nightmareArea {
		if a != (mapRect{}) && a.contains(x, y) {
			g.mobs[u.id].info.Nightmare[i] = wire.Position{X: uint16(x), Y: uint16(y)}
			return true
		}
	}
	return false
}

// Zakum -------------------------------------------------------------------

const (
	genZakum        = 3
	itemZakumEmblem = 751
	itemZakumPrize  = 753
	zakumMinutes    = 15
	langZakumBusy   = 222
	langZakumKilled = 163
	langZakumReset  = 164
)

var (
	zakumArea       = mapRect{0x884, 0x488, 0x8F8, 0x4D4}
	zakumNotice     = [4]int{0x880, 0x488, 0x8FC, 0x4FC}
	zakumKilledArea = [4]int{2176, 1160, 2300, 1276}
)

type zakumState struct {
	operating bool
	started   time.Time
	users     []int
	clear     int
}

// npcZakum is the NPC of the Zakum castle: the emblem takes the party in.
func (g *Game) npcZakum(u *User) {
	pl := &g.mobs[u.id].info.Player
	slot := firstSlotOf(pl, itemZakumEmblem)
	if slot < 0 {
		g.clientMessage(u, "Traga-me o Emblema do Dragão para ter acesso ao Castelo de Zakum")
		return
	}
	z := &g.zakum
	if z.operating && len(z.users) != 0 {
		g.clientMessage(u, g.lang(langZakumBusy), g.charName(z.users[len(z.users)-1]), len(z.users))
		return
	}
	leader := g.mobs[u.id].npc.leader
	if leader == 0 {
		leader = u.id
	}
	z.users = z.users[:0]
	for _, id := range g.partyMembers(leader) {
		if id != leader && id > 0 && id < MaxPlayer && g.users[id].status == userPlay {
			z.users = append(z.users, id)
		}
	}
	z.users = append(z.users, leader)
	for _, id := range z.users {
		g.teleportar(id, 2241, 1261)
	}
	z.started, z.operating = time.Now(), true
	amountMinus(&pl.Inventory[slot])
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
}

func (g *Game) zakumUnregister(id int) {
	g.zakum.users = slices.DeleteFunc(g.zakum.users, func(x int) bool { return x == id })
}

// zakumTimer ends the quest after 15 minutes or when it is empty.
func (g *Game) zakumTimer() {
	z := &g.zakum
	if z.operating && (time.Since(z.started) >= zakumMinutes*time.Minute || len(z.users) == 0) {
		g.clearArea(zakumArea)
		z.operating = false
	}
}

func (g *Game) zakumKilled(killer int) {
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	if killer < MaxPlayer {
		g.noticeArea(fmt.Sprintf(g.lang(langZakumKilled), g.charName(killer)), zakumKilledArea)
	}
	g.zakum.clear = 1
}

// zakumMinute is the reset that follows the boss's death (ProcessMinTimer).
func (g *Game) zakumMinute() {
	z := &g.zakum
	switch z.clear {
	case 1:
		z.clear = 2
		g.noticeArea(g.lang(langZakumReset), zakumNotice)
	case 2:
		z.clear, z.operating = 0, false
		g.clearArea(zakumArea)
	}
}

// Weather -----------------------------------------------------------------

const opSrvWeather = 0x18B

func (g *Game) sendWeather() {
	p := &wire.Parm1{Header: wire.NewHeader(opSrvWeather, wire.ClientIDSystem), Parm1: int32(g.weather)}
	g.forEachPlaying(func(u *User) { g.queue(u, p) })
}

// weatherTimer is the weather part of ProcessMinTimer.
func (g *Game) weatherTimer() {
	if g.forceWeather != -1 {
		if g.weather != g.forceWeather {
			g.weather, g.forceWeather = g.forceWeather, -1
			g.sendWeather()
		}
		return
	}
	switch r := rnd() % 0x4B0; {
	case r < 260 && g.weather != 0:
		g.weather = 0
		g.sendWeather()
	case r > 30 && r < 50 && g.weather != 1:
		g.weather = 1
		g.sendWeather()
	case r > 55 && r < 60 && g.weather != 2:
		g.weather = 2
		g.sendWeather()
	}
}

// MAC bans ----------------------------------------------------------------

// SetMacBans replaces the list of banned computers.
func (g *Game) SetMacBans(macs [][8]byte) {
	g.post(event{kind: evMacBans, macs: macs})
}

func (g *Game) banMac(u, target *User) {
	mac := target.mac
	g.macBans[mac] = true
	g.sendDB(&wire.MacBan{Header: wire.NewHeader(wire.OpMacBan, 0), Mac: mac, Type: 1})
	for _, o := range g.users[1:] {
		if o.status != userEmpty && (o.mac == mac || o.ip == target.ip) {
			g.clientMessage(o, "Você foi banido.")
			g.flush(o)
			g.closeUser(o)
		}
	}
	g.log.Warn("MAC banned", "by", u.id, "mac", fmt.Sprintf("%X", mac))
	g.adminSay("O usuário foi banido e desconectado do servidor.")
}

// Timed bosses (ReadBoss, ProcessSecTimer) --------------------------------

type bossState struct {
	current int // generator alive, 0 = waiting
	mob     int
	last    time.Time
}

func (g *Game) bossTimer(now time.Time) {
	if len(g.bosses) != len(g.cfg.Bosses) {
		g.bosses = make([]bossState, len(g.cfg.Bosses))
		for i := range g.bosses {
			g.bosses[i].last = now
		}
	}
	for i := range g.bosses {
		b, cfg := &g.bosses[i], &g.cfg.Bosses[i]
		if b.current == 0 {
			if now.Sub(b.last) < time.Duration(cfg.RebornSeconds)*time.Second {
				continue
			}
			gen := cfg.Geners[rnd()%len(cfg.Geners)]
			b.last, b.current, b.mob = now, gen, 0
			g.generateMob(gen, 0, 0)
			if gen > 0 && gen < len(g.data.Geners) {
				g.notice(fmt.Sprintf("Boss %s renasceu", wire.CString(g.data.Geners[gen].Leader.Name[:])))
			}
			continue
		}
		if now.Sub(b.last) < time.Duration(cfg.MaxSeconds)*time.Second {
			continue
		}
		if b.mob != 0 && g.mobs[b.mob].mode == modeBattle {
			continue
		}
		for id := firstNPC; id < MaxMob; id++ {
			if g.mobs[id].mode != 0 && g.mobs[id].npc.generateID == b.current {
				g.deleteMob(id, 34)
			}
		}
		b.last, b.current, b.mob = now, 0, 0
	}
}

func (g *Game) bossSpawned(gen, id int) {
	for i := range g.bosses {
		if b := &g.bosses[i]; b.current == gen && b.mob == 0 {
			b.mob = id
			return
		}
	}
}

func (g *Game) bossKilled(target, killer int) {
	for i := range g.bosses {
		b := &g.bosses[i]
		if b.mob != target || b.current == 0 {
			continue
		}
		if owner := g.summonOwner(killer); owner != 0 {
			killer = owner
		}
		name := wire.CString(g.mobs[target].info.Player.Name[:])
		guild := 0
		if killer < MaxPlayer {
			guild = int(g.mobs[killer].info.Player.GuildIndex)
		}
		if guild != 0 {
			g.notice(fmt.Sprintf("O jogador %s da guild %s derrotou %s", g.charName(killer), g.guildName(guild), name))
			if f := g.cfg.Bosses[i].Fame; f != 0 {
				g.setGuildFame(guild, g.guild(guild).Fame+f)
			}
		} else {
			g.notice(fmt.Sprintf("O jogador %s derrotou o %s", g.charName(killer), name))
		}
		b.current, b.mob, b.last = 0, 0, time.Now()
		return
	}
}

// The hit event (CEventHit, NPC 62 "Yugi") --------------------------------

const (
	hitEventBoss  = "HIT-HERE"
	hitEventPrize = 475
	hitEventHits  = 300
)

func hitEventArea(t wire.Position) bool { return t.X/128 == 10 && t.Y/128 == 11 }

func hitEventFairy(id uint16) bool { return id == 3908 || id == 3915 }

// hitEventEntry is CEventHit::entry.
func (g *Game) hitEventEntry(u *User) {
	same := 0
	for _, o := range g.users[1:] {
		if o != u && o.status == userPlay && hitEventArea(g.mobs[o.id].target) {
			if o.mac == u.mac {
				g.clientMessage(u, "Somente uma conta por PC.")
				return
			}
			if o.ip == u.ip {
				same++
			}
		}
	}
	if same > 1 {
		g.clientMessage(u, "Somente 1 IP por rede.")
		return
	}
	if lv := g.mobs[u.id].info.Player.Status.Level; lv <= 400 && lv != 0 {
		g.clientMessage(u, "Somente personagem nível 1 pode entrar na área do evento.")
		return
	}
	pos := [2][2]int{{1364, 1466}, {1356, 1452}}[rnd()%2]
	g.teleportar(u.id, pos[0], pos[1])
}

// hitEventProgress is CEventHit::progress: true when the hit was for the event.
func (g *Game) hitEventProgress(u *User, target int) bool {
	m := &g.mobs[u.id]
	if target < firstNPC || !hitEventArea(m.target) || g.charName(target) != hitEventBoss {
		return false
	}
	need := hitEventHits
	switch m.info.Player.Equip[13].Index {
	case 3908:
		need = 200
	case 3915:
		need = 150
	}
	u.hitEventCount++
	g.queue(u, &wire.Parm1{Header: wire.NewHeader(opSrvCounterMob, uint16(u.id)), Parm1: int32(u.hitEventCount) | int32(need)<<16})
	if u.hitEventCount < need {
		return true
	}
	u.hitEventCount = 0
	prize := wire.Item{Index: hitEventPrize}
	prize.Effect[0] = wire.ItemEffect{Index: 61, Value: 1}
	defer g.eventWatch(u, "Evento de hits: "+hitEventBoss)()
	g.grantGrouped(u, &prize, hitEventFairy(m.info.Player.Equip[13].Index))
	return true
}

// grantGrouped gives an item, grouped with what the player has when fairy
// is set, and shows it.
func (g *Game) grantGrouped(u *User, it *wire.Item, fairy bool) {
	pl := &g.mobs[u.id].info.Player
	slot := firstSlotOf(pl, it.Index)
	if slot >= 0 && fairy && g.agroupItemFada(u, it) {
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	} else if !g.putItem(u, it) {
		g.clientMessage(u, "Não foi possível adicionar o item ao inventário.")
		return
	}
	g.sendCarry(u)
	g.sendEmotion(u.id, 14, 3)
}

// Special shop items (RequestBuyShop.cpp) ---------------------------------

const itemArenaCoupon = 4132

var (
	questTeleports = map[uint16]struct {
		x, y int
		name string
	}{
		4038: {2371, 2101, "do Coveiro"}, 4039: {2224, 1712, "Jardim de Deus"}, 4040: {454, 3910, "Kaizen"},
		4041: {668, 3769, "Hidra"}, 4042: {1300, 4036, "Elfos"},
	}
	arenaShop = map[uint16]struct {
		coupons int
		effects [3]wire.ItemEffect
	}{
		4140: {5, [3]wire.ItemEffect{{Index: 61, Value: 1}}},
		3314: {2, [3]wire.ItemEffect{{Index: 61, Value: 1}}},
		3173: {5, [3]wire.ItemEffect{{Index: 61, Value: 3}}},
		4171: {coupons: 30},
		4172: {coupons: 30},
		4211: {coupons: 20},
		3915: {5, [3]wire.ItemEffect{{Index: 106, Value: 1}, {Index: 107, Value: 1}, {Index: 108, Value: 1}}},
		591:  {40, [3]wire.ItemEffect{{Index: 43, Value: 9}, {Index: 60, Value: 15}}},
		592:  {40, [3]wire.ItemEffect{{Index: 43, Value: 9}, {Index: 2, Value: 45}}},
	}
	eventBoxes = []uint16{475, 476, 480, 482, 483, 484}
)

// shopSpecial handles the shop items that are not sold for gold: quest
// teleports, event boxes and the Arena shop. True when it was one of them.
func (g *Game) shopSpecial(u *User, shop int, item *wire.Item) bool {
	name := g.charName(shop)
	if q, ok := questTeleports[item.Index]; ok && name == "Teleporte Quest" {
		g.clientMessage(u, "Bem Vindo a Quest %s!", q.name)
		g.teleportar(u.id, q.x, q.y)
		return true
	}
	if slices.Contains(eventBoxes, item.Index) {
		g.openEventBox(u, item.Index)
		return true
	}
	if a, ok := arenaShop[item.Index]; ok && name == "Loja Arena" {
		g.buyArenaShop(u, item.Index, a.coupons, a.effects)
		return true
	}
	return false
}

// openEventBox exchanges a box for a prize drawn by chance.
func (g *Game) openEventBox(u *User, box uint16) {
	pl := &g.mobs[u.id].info.Player
	slot := firstSlotOf(pl, box)
	if slot < 0 {
		g.clientMessage(u, "Você não tem o item %s.", g.itemName(box))
		return
	}
	if firstEmptySlot(pl) == -1 {
		g.clientMessage(u, "Seu inventário está cheio.")
		return
	}
	if amount := itemAmount(&pl.Inventory[slot]); amount > 1 {
		g.removeAmount(u, box, 1)
	} else {
		amountMinus(&pl.Inventory[slot])
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	}
	prize := wire.Item{}
	prize.Effect[0] = wire.ItemEffect{Index: 61, Value: 1}
	switch r := rnd() % 100; {
	case r < 40:
		prize.Index = 480
	case r < 55:
		prize.Index = 4020
	case r < 70:
		prize.Index = 578
	case r < 85:
		prize.Index = 1774
	default:
		prize.Index = 4026
		prize.Effect[0].Value = 2
	}
	g.grantGrouped(u, &prize, pl.Equip[13].Index == 3915)
}

func (g *Game) buyArenaShop(u *User, id uint16, coupons int, effects [3]wire.ItemEffect) {
	pl := &g.mobs[u.id].info.Player
	if inventoryAmount(pl, itemArenaCoupon) < coupons {
		g.clientMessage(u, "Você precisa de %d cupons Arena para compor !", coupons)
		return
	}
	if firstEmptySlot(pl) == -1 {
		g.clientMessage(u, "Seu inventário está cheio.")
		return
	}
	g.removeAmount(u, itemArenaCoupon, coupons)
	it := wire.Item{Index: id, Effect: effects}
	g.putItem(u, &it)
	g.sendCarry(u)
	g.sendEtc(u)
	g.sendEmotion(u.id, 14, 3)
	g.clientMessage(u, "O item %s chegou no seu inventário.", g.itemName(id))
}

// Kefra's area attack ------------------------------------------------------

// areaAttack is the 0x2000 branch of the battle loop: the mob strikes up to
// 13 players around it.
func (g *Game) areaAttack(id int) {
	m := &g.mobs[id]
	vision := min(int(m.info.Player.Status.DEX), 20)
	cx, cy := int(m.target.X), int(m.target.Y)
	var targets []int
	for y := cy - vision; y < cy+vision && len(targets) < 13; y++ {
		for x := cx - vision; x < cx+vision && len(targets) < 13; x++ {
			if !inMap(x, y) {
				continue
			}
			t := int(g.cell(x, y))
			if t <= 0 || t >= MaxPlayer || g.mobs[t].info.Player.Status.CurHP <= 0 || rnd()%5 >= 3 {
				continue
			}
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		return
	}
	multi := g.getAttack(id, targets[0])
	multi.Header.PacketID, multi.Header.Size = wire.OpCliAttack, attackSize
	for i, t := range targets {
		one := multi
		if i != 0 {
			one = g.getAttack(id, t)
		}
		multi.Target[i].Index = int32(t)
		multi.Target[i].Damage = int64(g.mobStrike(id, t, int(one.Target[0].Damage)))
	}
	raw, err := wire.Marshal(&multi)
	if err != nil {
		return
	}
	g.multicastRaw(cx, cy, raw[:attackSize])
	for _, t := range targets {
		if s := &g.mobs[t].info.Player.Status; s.CurHP <= 0 {
			s.CurHP = 0
			g.mobKilled(t, id)
		}
	}
}

// Timers of ProcessSecTimer ------------------------------------------------

const (
	idleLimitTicks = 720 // CheckIdle: 6 minutes at 500 ms a tick
	bigCuboKills   = 250 // lanhouseN totalToKill
	bigCuboPrize   = 475
)

var (
	bigCuboArea = mapRect{1304, 1438, 1379, 1473}
	valleyArea  = mapRect{2177, 3585, 2304, 3839}
	questAreas  = []mapRect{
		{2380, 2078, 2425, 2133}, {2229, 1701, 2256, 1728}, {459, 3886, 500, 3917}, {657, 3728, 704, 3764},
		{1308, 4023, 1350, 4057}, {793, 4046, 828, 4080}, {1718, 1714, 1745, 1738}, {2232, 1564, 2256, 1589},
		{2639, 1966, 2670, 2004}, {1949, 1586, 1989, 1615}, {301, 407, 330, 487},
	}
)

// serverAreaTimer: quests reset every 10 minutes, the valley every 30, and
// nobody stays in the valley without its fairy.
func (g *Game) serverAreaTimer() {
	if g.secCounter%120 == 0 {
		g.fairyMinuteTimer()
	}
	if g.secCounter%1200 == 0 {
		for _, a := range questAreas {
			g.clearArea(a)
		}
		g.forEachPlaying(func(u *User) {
			u.questAccess = 0
			g.chatTo(u, colorGreen, "[Serve info] : Todas as quests foram resetadas")
		})
	}
	if g.secCounter%3600 == 0 {
		g.clearArea(valleyArea)
	}
	if g.secCounter%4 == 0 {
		g.forEachPlaying(func(u *User) {
			m := &g.mobs[u.id]
			if fairy := m.info.Player.Equip[13].Index; insideValley(m.target) && fairy != 3916 && fairy != 3917 {
				g.doRecall(u.id)
			}
		})
	}
}

// idleTimer is CheckIdle: a client silent for 6 minutes is disconnected.
func (g *Game) idleTimer() {
	for _, u := range g.users[1:] {
		if u.status != userEmpty && !u.store.open && u.conn != nil && g.secCounter-u.lastReceiveSec > idleLimitTicks {
			g.log.Info("disconnected for inactivity", "slot", u.id)
			g.closeUser(u)
		}
	}
}

// bigCuboKill: every 250 kills in the BigCubo, everyone there gets a box.
func (g *Game) bigCuboKill(u *User) {
	t := g.mobs[u.id].target
	if !bigCuboArea.contains(int(t.X), int(t.Y)) {
		return
	}
	g.bigCuboCount++
	g.queue(u, &wire.Parm1{Header: wire.NewHeader(opSrvCounterMob, uint16(u.id)), Parm1: int32(g.bigCuboCount) | bigCuboKills<<16})
	if g.bigCuboCount < bigCuboKills {
		return
	}
	g.bigCuboCount = 0
	g.forEachPlaying(func(o *User) {
		p := g.mobs[o.id].target
		if bigCuboArea.contains(int(p.X), int(p.Y)) {
			prize := wire.Item{Index: bigCuboPrize}
			g.eventItem(o, &prize, "Big Cubo")
			g.sendCarry(o)
			g.sendEtc(o)
			g.clientMessage(o, "Parabéns participantes, todos foram recompensados com BAU I")
		}
	})
}

// Special monsters of MobKilled --------------------------------------------

const (
	genBossFrost    = 4807
	genAlmaUni      = 4067
	genAlmaUniEnd   = 4086
	genAlmaFenix    = 3658
	genAlmaFenixEnd = 3743
	almaKills       = 500
)

// gateTowerKilled: a gate tower (face 219) opens the gate it stands on.
func (g *Game) gateTowerKilled(target int) {
	m := &g.mobs[target]
	gen := m.npc.generateID
	if m.info.Player.Equip[0].Index != 219 || gen == genTowerGuardB || gen == genTowerGuardR {
		return
	}
	id := g.itemAt(int(m.target.X), int(m.target.Y))
	if id <= 0 || id >= maxGroundItem || g.ground[id].item.Index == 0 {
		return
	}
	if _, ok := g.updateGroundItem(id, gateOpen); ok {
		g.broadcastGate(id, 0)
		g.ground[id].closeArmed = false
	}
}

func (g *Game) giveBonusItem(u *User, event string, id uint16, level, sanc int) {
	it := wire.Item{Index: id}
	g.setItemBonusLevel(&it, level, sanc, 0)
	g.eventItem(u, &it, event)
}

// monsterPrize is what some monsters hand directly to whoever killed them.
func (g *Game) monsterPrize(gen, killer int) {
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	if killer <= 0 || killer >= MaxPlayer || g.users[killer].status != userPlay {
		return
	}
	u := g.users[killer]
	name := g.charName(killer)
	event := "Prêmio de monstro"
	if gen >= 0 && gen < len(g.data.Geners) {
		if n := wire.DecodeGame(wire.CString(g.data.Geners[gen].Leader.Name[:])); n != "" {
			event = n
		}
	}
	switch {
	case gen == 0 || gen == 1 || gen == 2:
		switch rnd() % 14 {
		case 0:
			g.giveBonusItem(u, event, 420, 0, 0)
		case 1:
			g.giveBonusItem(u, event, 421, 0, 0)
		}
	case gen == 5 || gen == 6 || gen == 7:
		if rnd()%14 == 0 {
			g.giveBonusItem(u, event, uint16(rnd()%7+421), 0, 0)
		}
	case gen == genZakum:
		if id := [7]uint16{1106, 1256, 1418, 1568}[rnd()%7]; id != 0 {
			g.giveBonusItem(u, event, id, 75, 1)
		}
	case gen == genBossFrost:
		it := wire.Item{}
		it.Effect[0] = wire.ItemEffect{Index: efAmount, Value: 1}
		switch r := rnd() % 100; {
		case r < 35:
			it.Index = 4029
		case r < 55:
			it.Index, it.Effect[0].Value = 3314, 3
		case r < 75:
			it.Index, it.Effect[0].Value = 4140, 2
		default:
			it.Index, it.Effect[0].Value = 4140, 2
		}
		g.eventItem(u, &it, event)
		u.account.Honra += 20
		g.sendEtc(u)
		msg := fmt.Sprintf("O jogador %s derrotou o Boss %s", name, wire.DecodeGame(wire.CString(g.data.Geners[gen].Leader.Name[:])))
		g.notice(msg)
		g.chatAll(arenaAnnounce, msg)
	case gen >= genAlmaUni && gen <= genAlmaUniEnd:
		g.almaKill(u, event, &g.uniCount, 1740, fmt.Sprintf("O jogador %s dropou Alma do Uni", name))
	case gen >= genAlmaFenix && gen <= genAlmaFenixEnd:
		g.almaKill(u, event, &g.phoenixCount, 1741, fmt.Sprintf("O jogador %s dropou Alma da Fênix", name))
	}
}

func (g *Game) almaKill(u *User, event string, count *int, item uint16, msg string) {
	*count++
	if *count < almaKills {
		return
	}
	*count = 0
	it := wire.Item{Index: item}
	g.eventItem(u, &it, event)
	g.notice(msg)
	g.chatAll(arenaAnnounce, msg)
}

// isBossMob: the mob is one of the timed bosses.
func (g *Game) isBossMob(id int) bool {
	return slices.ContainsFunc(g.bosses, func(b bossState) bool { return b.current != 0 && b.mob == id })
}
