package game

import (
	"fmt"
	"slices"
	"time"

	"dbsrv/internal/wire"
)

// The Arena Real (cArenaReal.cpp, UOD_Arena.h). Registration with Odin in
// the first five minutes of the hours in tmsrv.yaml; at minute 5 the players
// are split into four teams, strongest first, for up to 15 minutes. Dying
// respawns at the team corner and gives the killer one point; the first
// team to reach the goal wins.

const (
	arenaMaxPlayers   = 52
	arenaMinPlayers   = 4
	arenaTeams        = 4
	arenaMinutes      = 15
	arenaBasePoints   = 15
	arenaOpenMinutes  = 5
	arenaAnnounce     = uint32(0xFFFF7F27)
	arenaProtectAura  = 32 // removed when a player dies in the arena
	arenaRefusedHours = "Não está na hora da batalha ou está no limite de participantes"
)

var (
	arenaArea      = mapRect{143, 546, 195, 625}
	arenaStart     = [arenaTeams][2]int{{190, 621}, {190, 555}, {147, 555}, {147, 621}}
	arenaTeamNames = [arenaTeams]string{"Mestre", "Caçador", "Assasino", "Legendary"}
)

type arenaStatus int

const (
	arenaOff arenaStatus = iota
	arenaRegistering
	arenaRunning
)

type arenaState struct {
	status      arenaStatus
	registered  []int
	teams       [arenaTeams][]int
	pointsToWin int
	endAt       time.Time
	lastMessage time.Time
}

func (g *Game) arenaOpenHour(now time.Time) bool {
	return slices.Contains(g.cfg.Arena.Hours, now.Hour())
}

// arenaRegister signs u up and returns the answer for the player.
func (g *Game) arenaRegister(u *User, now time.Time) string {
	a := &g.arena
	name := g.charName(u.id)
	if len(a.registered) >= arenaMaxPlayers || !g.arenaOpenHour(now) || now.Minute() >= arenaOpenMinutes {
		return arenaRefusedHours
	}
	if slices.ContainsFunc(a.registered, func(id int) bool { return g.users[id].mac == u.mac }) {
		return fmt.Sprintf("Você já está registrado, %s", name)
	}
	a.registered = append(a.registered, u.id)
	g.sendClientInfo(u)
	g.log.Info("arena registered", "slot", u.id, "total", len(a.registered))
	g.chatAll(arenaAnnounce, fmt.Sprintf("%s registrou-se na arena. Total: %02d/%02d", name, len(a.registered), arenaMaxPlayers))
	return fmt.Sprintf("%s, você foi registrado. Total de registros: %d/%d", name, len(a.registered), arenaMaxPlayers)
}

// arenaUnregister takes out whoever leaves the game.
func (g *Game) arenaUnregister(u *User) {
	a := &g.arena
	isU := func(id int) bool { return id == u.id }
	a.registered = slices.DeleteFunc(a.registered, isU)
	if t := u.arenaTeam; t != 0 {
		a.teams[t-1] = slices.DeleteFunc(a.teams[t-1], isU)
		u.arenaTeam, u.arenaKills = 0, 0
		g.mobs[u.id].guildDisable = false
	}
}

func (g *Game) arenaTimer() { g.arenaTick(time.Now()) }

// arenaTick is TOD_Arena::Work, once a second.
func (g *Game) arenaTick(now time.Time) {
	a := &g.arena
	switch a.status {
	case arenaOff:
		if now.Minute() == 0 && g.arenaOpenHour(now) {
			g.chatAll(arenaAnnounce, "O registro para a Arena foi aberto! Participe falando com Odin, próximo ao PvP Armia")
			a.status, a.lastMessage = arenaRegistering, now
		}
	case arenaRegistering:
		if now.Minute() >= arenaOpenMinutes {
			g.startArena(now)
		} else if now.Sub(a.lastMessage) >= time.Minute {
			g.chatAll(arenaAnnounce, "Participe da Arena Real falando com o Odin, próximo ao PvP Armia!")
			a.lastMessage = now
		}
	case arenaRunning:
		if !now.Before(a.endAt) {
			g.decideArenaWinner()
			g.resetArena()
		}
	}
}

func (g *Game) startArena(now time.Time) {
	a := &g.arena
	total := len(a.registered)
	if total < arenaMinPlayers {
		g.log.Info("arena not started: too few players", "total", total)
		g.chatAll(arenaAnnounce, "Arena não iniciada por falta de participantes")
		g.resetArena()
		return
	}
	power := map[int]int{}
	for _, id := range a.registered {
		power[id] = g.overPower(&g.mobs[id].info)
	}
	slices.SortStableFunc(a.registered, func(x, y int) int { return power[y] - power[x] })
	for i, id := range a.registered {
		g.removeParty(id)
		a.teams[i%arenaTeams] = append(a.teams[i%arenaTeams], id)
	}
	a.registered = nil
	a.pointsToWin = arenaBasePoints
	if total > 12 {
		a.pointsToWin += (total - 12) / arenaTeams * 10
	}
	for t, team := range a.teams {
		g.formArenaTeam(t, team)
	}
	a.status, a.endAt = arenaRunning, now.Add(arenaMinutes*time.Minute)
	g.log.Info("arena started", "total", total, "points_to_win", a.pointsToWin)
}

// formArenaTeam makes the team a party and moves it to its corner.
func (g *Game) formArenaTeam(t int, team []int) {
	leader := team[0]
	lead := &g.mobs[leader].npc
	lead.leader, lead.followers = 0, [maxParty]int{}
	g.sendAddParty(leader, leader, true)
	next := 0
	for _, id := range team {
		u, m := g.users[id], &g.mobs[id]
		if id != leader {
			lead.followers[next] = id
			next++
			m.npc.leader = leader
		}
		g.sendAddParty(leader, id, false)
		g.sendAddParty(id, leader, true)
		for _, other := range team {
			if other != leader {
				g.sendAddParty(id, other, false)
			}
		}
		m.guildDisable = true
		u.arenaTeam, u.arenaKills = t+1, 0
		g.teleportar(id, arenaStart[t][0], arenaStart[t][1])
		g.signalParm(u, uint16(id), opQuestTime, arenaMinutes*60)
		g.sendClientInfo(u)
		g.showArenaTag(id)
	}
}

// showArenaTag sends the player again, so others see the team tag change.
func (g *Game) showArenaTag(id int) {
	p := g.createMobPacket(id)
	g.multicast2(int(g.mobs[id].target.X), int(g.mobs[id].target.Y), &p, 0)
}

// arenaTag is the "Grupo X (kills)" shown over a fighter inside the arena.
func (g *Game) arenaTag(id int) (string, bool) {
	if id <= 0 || id >= MaxPlayer || g.users[id].arenaTeam == 0 {
		return "", false
	}
	if t := g.mobs[id].target; !arenaArea.contains(int(t.X), int(t.Y)) {
		return "", false
	}
	u := g.users[id]
	return fmt.Sprintf("Grupo %s (%d)", arenaTeamNames[u.arenaTeam-1], u.arenaKills), true
}

// arenaMobKilled is TOD_Arena::MobKilled: true when the death was absorbed
// by the arena, so it costs nothing and the normal death does not happen.
func (g *Game) arenaMobKilled(target, killer int) bool {
	a := &g.arena
	if a.status != arenaRunning || target >= MaxPlayer || g.users[target].arenaTeam == 0 {
		return false
	}
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	u, m := g.users[target], &g.mobs[target]
	if i := slices.IndexFunc(m.info.Affects[:], func(e wire.Affect) bool { return e.Index == arenaProtectAura }); i >= 0 {
		m.info.Affects[i] = wire.Affect{}
	}
	if killer != target && killer < MaxPlayer && g.users[killer].arenaTeam != 0 {
		g.users[killer].arenaKills++
	}
	s := &m.info.Player.Status
	s.CurHP, s.CurMP = s.MaxHP, s.MaxMP
	u.potionHP, u.potionMP = s.MaxHP, s.MaxMP
	g.setReqHp(u)
	g.setReqMp(u)
	g.sendScore(target)
	start := arenaStart[u.arenaTeam-1]
	g.teleportar(target, start[0], start[1])
	g.showArenaTag(target)
	g.sendAffect(u)
	g.noticeArea(fmt.Sprintf(".%s da equipe %s foi morto por %s.", g.charName(target), arenaTeamNames[u.arenaTeam-1], g.charName(killer)),
		[4]int{arenaArea.minX, arenaArea.minY, arenaArea.maxX, arenaArea.maxY})
	g.checkArenaWinner()
	return true
}

func (g *Game) arenaPoints() [arenaTeams]int {
	var points [arenaTeams]int
	for t, team := range g.arena.teams {
		for _, id := range team {
			points[t] += g.users[id].arenaKills
		}
	}
	return points
}

func (g *Game) checkArenaWinner() {
	points := g.arenaPoints()
	winner := slices.IndexFunc(points[:], func(p int) bool { return p >= g.arena.pointsToWin })
	if winner < 0 {
		return
	}
	g.giveArenaRewards(winner)
	g.chatAll(arenaAnnounce, fmt.Sprintf("O grupo %s foi vencedor por possuir %d pontos.", arenaTeamNames[winner], points[winner]))
	g.log.Info("arena won", "team", arenaTeamNames[winner], "points", points[winner])
	g.resetArena()
}

// decideArenaWinner ends a match that ran out of time.
func (g *Game) decideArenaWinner() {
	points := g.arenaPoints()
	best := slices.Max(points[:])
	winner := slices.Index(points[:], best)
	for t, p := range points {
		if t != winner && p == best {
			g.notice("Nenhum grupo foi vencedor pois o número de vidas é igual")
			g.log.Info("arena ended in a tie", "points", best)
			return
		}
	}
	g.giveArenaRewards(winner)
	g.notice(fmt.Sprintf("O grupo %s foi vencedor por possuir %d pontos .", arenaTeamNames[winner], best))
	g.log.Info("arena won on time", "team", arenaTeamNames[winner], "points", best)
}

func (g *Game) giveArenaItems(u *User, items []ArenaItemSettings, why string) {
	for _, reward := range items {
		it := wire.Item{Index: uint16(reward.Item)}
		if reward.Amount > 0 {
			it.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint16(reward.Amount)}
		}
		if !g.putItem(u, &it) {
			g.clientMessage(u, "Sem espaço no inventário para o prêmio da Arena: [%s]", g.itemName(it.Index))
			g.log.Warn("arena reward lost: inventory full", "slot", u.id, "item", it.Index, "reason", why)
			continue
		}
		g.clientMessage(u, "Chegou um item: [%s]", g.itemName(it.Index))
		g.log.Info("arena reward", "slot", u.id, "item", it.Index, "amount", reward.Amount, "reason", why)
	}
}

// giveArenaRewards pays the winners, the top killer and whoever scored. A
// tie for top killer pays nobody that prize; in the C++ the first of the
// tied players also lost the participant prize.
func (g *Game) giveArenaRewards(winner int) {
	a := &g.arena
	for _, id := range a.teams[winner] {
		g.giveArenaItems(g.users[id], g.cfg.Arena.WinnerItems, "winner")
	}
	top, tied := 0, false
	for _, team := range a.teams {
		for _, id := range team {
			switch {
			case top == 0 || g.users[id].arenaKills > g.users[top].arenaKills:
				top, tied = id, false
			case g.users[id].arenaKills == g.users[top].arenaKills:
				tied = true
			}
		}
	}
	if tied {
		top = 0
	}
	if top != 0 {
		g.chatAll(arenaAnnounce, fmt.Sprintf("O usuário %s foi o Top Kill com %d kills", g.charName(top), g.users[top].arenaKills))
		g.giveArenaItems(g.users[top], g.cfg.Arena.TopKillItems, "top kill")
	}
	for _, team := range a.teams {
		for _, id := range team {
			u := g.users[id]
			if u.arenaKills <= 0 {
				g.clientMessage(u, "Não recebeu premiação de participante por não ter nenhuma kill")
			} else if id != top {
				g.giveArenaItems(u, g.cfg.Arena.ParticipantItems, "participant")
			}
		}
	}
}

// resetArena ends the event. The C++ left the guild hidden until the next
// login.
func (g *Game) resetArena() {
	a := &g.arena
	for _, team := range a.teams {
		for _, id := range team {
			u := g.users[id]
			u.arenaTeam, u.arenaKills = 0, 0
			g.mobs[id].guildDisable = false
			g.signalParm(u, uint16(id), opQuestTime, 0)
			g.sendClientInfo(u)
			g.showArenaTag(id)
		}
	}
	*a = arenaState{}
	g.clearArea(arenaArea)
}
