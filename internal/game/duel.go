package game

import (
	"fmt"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Duels in the ranking arena (RequestDuel.cpp, Basedef.cpp DoRanking /
// ProcessRanking, SendFunc.cpp SendDamage / SendEnvEffect /
// SendNoticeArea, ClearAreaTeleport). A player invites another (0x39F
// type 0: 1x1, 1: 5x5 guilds, 2: 10x10 guilds, 3: guild leaders - always
// refused, as in the C++); the invited player accepts with type 4. One
// battle at a time per channel: the fighters go to the two arena corners,
// the arena shrinks with damage zones near the end and the side with
// living players left wins.

const (
	opDuel         = 0x39F
	opSrvDuelTimer = 0x3A1 // pMsgSignal: seconds left
	opSrvEnvEffect = 0x3A2

	nnBattleInProgress = 193
	nnBattleStarted    = 194
	ssWinBy            = 196
	ssDraw             = 197

	duelInviteDelay = 2 * time.Second
)

// arena and lobby (where guild members are picked from) rectangles.
var (
	duelArena = [4]int{142, 4007, 195, 4082}
	duelLobby = [4]int{2564, 1688, 2579, 1714}
	duelExit  = [2]int{0xA0C, 0x6D8}
)

// Duel mirrors p39F (20 bytes).
type Duel struct {
	Header wire.Header
	MobID  int32
	Type   int32
}

// EnvEffect mirrors p3A2 (24 bytes).
type EnvEffect struct {
	Header       wire.Header
	MinX, MinY   int16
	MaxX, MaxY   int16
	Type1, Type2 int16
}

// rankingState is sServer.Challanger.
type rankingState struct {
	process      int // 0 none, type + 1
	timer        int // ProcessRanking turns (2 s) left
	side1, side2 int // players (1x1) or guilds
	names        [2]string
}

func in(r [4]int, x, y int) bool { return x >= r[0] && x <= r[2] && y >= r[1] && y <= r[3] }

// requestDuel is CUser::RequestDuel (0x39F).
func (g *Game) requestDuel(u *User, pkt protocol.Packet) {
	var p Duel
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	other, typ := int(p.MobID), int(p.Type)
	if typ < 0 || typ > 4 || other <= 0 || other >= MaxPlayer || other == u.id {
		return
	}
	o := g.users[other]
	if o.status != userPlay {
		return
	}
	if o.whisperOff {
		g.clientMessage(u, "O outro jogador esta com whisper ativo")
		return
	}
	switch typ {
	case 3:
		g.clientMessage(u, "Disponível apenas para líderes de guild")
		return
	case 4: // accept: `other` is who invited
		if o.challengeTo != u.id {
			return
		}
		if g.ranking.process != 0 {
			g.clientMessage(u, "%s", g.lang(nnBattleInProgress))
			g.clientMessage(o, "%s", g.lang(nnBattleInProgress))
			return
		}
		kind := o.challengeType
		if kind < 0 || kind > 2 {
			return
		}
		o.challengeTo = 0
		g.sendEtc(u)
		g.sendEtc(o)
		g.doRanking(kind, u.id, other)
		return
	}
	if now := time.Now(); now.Sub(u.lastDuel) <= duelInviteDelay {
		g.clientMessage(u, "Aguarde para enviar novamente")
		u.lastDuel = now
		return
	}
	if (typ == 1 || typ == 2) && (g.mobs[u.id].info.Player.Status.CurHP <= 0 || g.mobs[other].info.Player.Status.CurHP <= 0) {
		return
	}
	u.challengeTo, u.challengeType = other, typ
	p.MobID = int32(u.id)
	p.Header.ClientID = uint16(other)
	g.queue(o, &p)
	u.lastDuel = time.Now()
	g.log.Info("duel invite", "slot", u.id, "to", other, "type", typ)
}

// doRanking is DoRanking: start the battle. Type 0 sends the two players;
// types 1/2 take up to 5/10 members of each guild from the lobby.
func (g *Game) doRanking(kind, a, b int) {
	pa, pb := &g.mobs[a].info.Player, &g.mobs[b].info.Player
	r := &g.ranking
	r.names = [2]string{wire.CString(pa.Name[:]), wire.CString(pb.Name[:])}
	start := func(id, x, y, timer int) {
		g.teleportar(id, x, y)
		if u := g.player(id); u != nil {
			g.clientMessage(u, "%s", g.lang(nnBattleStarted))
			g.signalParm(u, wire.ClientIDSystem, opSrvDuelTimer, int32(timer+timer-2))
		}
	}
	if kind == 0 {
		*r = rankingState{process: 1, timer: 121, side1: a, side2: b, names: r.names}
		start(a, 147, 4045, 121)
		start(b, 189, 4045, 121)
		return
	}
	ga, gb := int(pa.GuildIndex), int(pb.GuildIndex)
	if ga <= 0 || gb <= 0 {
		return
	}
	*r = rankingState{process: kind + 1, timer: 301, side1: ga, side2: gb, names: r.names}
	left := [2]int{5, 5}
	if kind == 2 {
		left = [2]int{10, 10}
	}
	for y := duelLobby[1]; y <= duelLobby[3]; y++ {
		for x := duelLobby[0]; x <= duelLobby[2]; x++ {
			id := int(g.cell(x, y))
			if id <= 0 || id >= MaxPlayer {
				continue
			}
			switch gi := int(g.mobs[id].info.Player.GuildIndex); {
			case left[0] > 0 && gi == ga:
				left[0]--
				start(id, 147, 4045, 301)
			case left[1] > 0 && gi == gb:
				left[1]--
				start(id, 189, 4045, 301)
			}
		}
	}
}

// rankingSides counts the living fighters of each side in the arena.
func (g *Game) rankingSides() (int, int) {
	r := &g.ranking
	n1, n2 := 0, 0
	for _, u := range g.users[1:] {
		if u.status != userPlay {
			continue
		}
		m := &g.mobs[u.id]
		if m.info.Player.Status.CurHP <= 0 || !in(duelArena, int(m.target.X), int(m.target.Y)) {
			continue
		}
		side := u.id
		if r.process != 1 {
			side = int(m.info.Player.GuildIndex)
		}
		if side == r.side1 {
			n1++
		}
		if side == r.side2 {
			n2++
		}
	}
	return n1, n2
}

// rankingTimer is ProcessRanking (every 4 sec ticks).
func (g *Game) rankingTimer() {
	r := &g.ranking
	if r.timer <= 0 {
		return
	}
	r.timer--
	if r.timer%5 == 0 {
		if n1, n2 := g.rankingSides(); n1 == 0 || n2 == 0 {
			r.timer = 0
		}
	}
	// The arena closes in: damage zones and their warning effects.
	type zone [4]int
	dmg, fx := []zone{}, []zone{}
	switch {
	case r.timer < 60:
		dmg = []zone{{0x8E, 0xFA7, 0xC3, 0xFCA}, {0x8E, 0xFCE, 0xC3, 0xFF2}}
		fx = []zone{{0x8E, 0xFA7, 0xA8, 0xFB2}, {0x8E, 0xFE7, 0xA8, 0xFF2}, {0x8E, 0xFB3, 0xA8, 0xFBE}, {0x8E, 0xFDB, 0xA8, 0xFE6},
			{0x8E, 0xFBF, 0xA8, 0xFCA}, {0x8E, 0xFCF, 0xA8, 0xFDA}, {0xA8, 0xFA7, 0xC3, 0xFB2}, {0xA8, 0xFE7, 0xC3, 0xFF2},
			{0xA8, 0xFB3, 0xC3, 0xFBE}, {0xA8, 0xFDB, 0xC3, 0xFE6}, {0xA8, 0xFBF, 0xC3, 0xFCA}, {0xA8, 0xFCF, 0xC3, 0xFDA}}
	case r.timer < 120:
		dmg = []zone{{0x8E, 0xFA7, 0xC3, 0xFC2}, {0x8E, 0xFD7, 0xC3, 0xFF2}}
		fx = []zone{{0x8E, 0xFA7, 0xA8, 0xFB2}, {0x8E, 0xFE7, 0xA8, 0xFF2}, {0x8E, 0xFB3, 0xA8, 0xFBE}, {0x8E, 0xFDB, 0xA8, 0xFE6},
			{0xA8, 0xFA7, 0xC3, 0xFB2}, {0xA8, 0xFE7, 0xC3, 0xFF2}, {0xA8, 0xFB3, 0xC3, 0xFBE}, {0xA8, 0xFDB, 0xC3, 0xFE6}}
	case r.timer < 180:
		dmg = []zone{{0x8E, 0xFA7, 0xC3, 0xFB3}, {0x8E, 0xFE6, 0xCE, 0xFF2}}
		fx = []zone{{0x8E, 0xFA7, 0xA8, 0xFB2}, {0x8E, 0xFE7, 0xA8, 0xFF2}, {0xA9, 0xFA7, 0xC3, 0xFB2}, {0xA9, 0xFE7, 0xC3, 0xFF2}}
	}
	for _, z := range dmg {
		g.areaDamage(z)
	}
	for _, z := range fx {
		g.envEffect(z, 0x20, 0)
	}
	if r.timer > 0 {
		return
	}
	n1, n2 := g.rankingSides()
	msg := ""
	switch {
	case n1 == n2:
		msg = fmt.Sprintf(cFormat(g.lang(ssDraw)), r.names[0], r.names[1])
	case n1 > n2:
		msg = fmt.Sprintf(cFormat(g.lang(ssWinBy)), r.names[0], r.names[1])
	default:
		msg = fmt.Sprintf(cFormat(g.lang(ssWinBy)), r.names[1], r.names[0])
	}
	g.noticeArea(msg, [4]int{0xA04, 0x698, 0xA13, 0x6B2})
	g.noticeArea(msg, duelArena)
	g.log.Info("duel ended", "result", msg)
	*r = rankingState{}
	g.clearAreaTeleport(duelArena, duelExit[0], duelExit[1])
}

// areaDamage is SendDamage: players in the rectangle drop to a quarter of
// their HP (at most 2000 lost, never below 1).
func (g *Game) areaDamage(z [4]int) {
	for _, u := range g.users[1:] {
		if u.status != userPlay {
			continue
		}
		m := &g.mobs[u.id]
		if !in(z, int(m.target.X), int(m.target.Y)) {
			continue
		}
		cur := m.info.Player.Status.CurHP
		hp := max(int32(float32(cur)*0.25), cur-2000, 1)
		diff := hp - cur
		m.info.Player.Status.CurHP = hp
		u.potionHP += diff
		g.setReqHp(u)
		g.setReqMp(u)
		g.multicast2(int(m.target.X), int(m.target.Y), &wire.HpChange{
			Header: wire.NewHeader(wire.OpSrvHpChange, uint16(u.id)), CurHP: hp, Incress: int16(diff)}, 0)
	}
}

// envEffect is SendEnvEffect.
func (g *Game) envEffect(z [4]int, t1, t2 int) {
	p := EnvEffect{Header: wire.NewHeader(opSrvEnvEffect, wire.ClientIDSystem),
		MinX: int16(z[0]), MinY: int16(z[1]), MaxX: int16(z[2]), MaxY: int16(z[3]), Type1: int16(t1), Type2: int16(t2)}
	g.multicast2(z[0]+(z[2]-z[0])>>1, z[1]+(z[3]-z[1])>>1, &p, 0)
}

// noticeArea is SendNoticeArea.
func (g *Game) noticeArea(msg string, z [4]int) {
	for _, u := range g.users[1:] {
		if u.status == userPlay && in(z, int(g.mobs[u.id].target.X), int(g.mobs[u.id].target.Y)) {
			g.clientMessage(u, "%s", msg)
		}
	}
}

// clearAreaTeleport is ClearAreaTeleport: everyone in the rectangle (the
// dead with 1 HP) goes to (x, y).
func (g *Game) clearAreaTeleport(z [4]int, x, y int) {
	for _, u := range g.users[1:] {
		m := &g.mobs[u.id]
		if u.status != userPlay || m.mode == 0 || !in(z, int(m.target.X), int(m.target.Y)) {
			continue
		}
		if m.info.Player.Status.CurHP <= 0 {
			m.info.Player.Status.CurHP = 1
			g.sendScore(u.id)
		}
		g.teleportar(u.id, x, y)
	}
}
