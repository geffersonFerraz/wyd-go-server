package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Parties (RequestAddParty 0x37F, RequestAcceptParty 0x3AB,
// RequestExitParty 0x37E, SendAddParty 0x37D, SendRemoveParty 0x37E,
// RemoveParty). A party is a leader (npc.leader == 0) whose npc.followers
// (PartyList[12]) hold the members; each member's npc.leader is the leader.
//
// Differences from the C++: an invite must come from the inviter itself
// (the C++ trusted the leaderId in the packet), accepting needs a pending
// invite from that leader (the C++ let anyone join any party), and a
// leader can only kick its own members.
// The custom group finder (passwords, lists) is in groupfinder.go.

const (
	maxParty      = 12
	inviteTimeout = 2 * time.Minute
)

// sendAddParty is SendAddParty(target, whom, leader): show whom in target's
// party window (HP as a percentage, as the C++ did).
func (g *Game) sendAddParty(target, whom int, leader bool) {
	u := g.player(target)
	if u == nil || u.status != userPlay {
		return
	}
	pl := &g.mobs[whom].info.Player
	p := wire.PartyMember{Header: wire.NewHeader(wire.OpSrvPartyAdd, wire.ClientIDSystem),
		PartyID: uint16(whom), Level: uint16(pl.BStatus.Level), MaxHP: 100, ID: 52428}
	if pl.Status.MaxHP > 0 {
		p.CurHP = uint16(float32(pl.Status.CurHP) / float32(pl.Status.MaxHP) * 100)
	}
	if !leader {
		p.LeaderID = wire.ClientIDSystem
	}
	copy(p.Name[:15], pl.Name[:])
	g.queue(u, &p)
}

// sendRemoveParty is SendRemoveParty(target, whom): 0 = target left.
func (g *Game) sendRemoveParty(target, whom int) {
	u := g.player(target)
	if u == nil || u.status != userPlay {
		return
	}
	g.queue(u, &wire.PartyExit{Header: wire.NewHeader(wire.OpCliPartyExit, wire.ClientIDSystem), MobID: int16(whom)})
}

func (g *Game) isLeaderWithParty(id int) bool {
	for _, f := range g.mobs[id].npc.followers {
		if f != 0 {
			return true
		}
	}
	return false
}

// requestAddParty is RequestAddParty: forward the invite to the target.
func (g *Game) requestAddParty(u *User, pkt protocol.Packet) {
	var p wire.PartyInvite
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	target := int(p.TargetID)
	if target <= 0 || target >= MaxPlayer || target == u.id || int(p.LeaderID) != u.id {
		return
	}
	t := g.users[target]
	if t.status != userPlay {
		return
	}
	switch {
	case g.mobs[u.id].npc.leader != 0:
		g.clientMessage(u, "Saia do grupo para criar um.")
		return
	case g.mobs[target].npc.leader != 0:
		g.clientMessage(u, "O outro jogador possui grupo.")
		return
	case g.isLeaderWithParty(target):
		g.clientMessage(u, "O outro jogador é líder de um grupo.")
		return
	}
	t.partyInvite, t.partyInviteAt = u.id, time.Now()
	p.PartyID, p.Unk = 1, 204
	p.Header.ClientID = wire.ClientIDSystem
	g.queue(t, &p)
}

// requestAcceptParty is RequestAcceptParty: join the leader's party.
func (g *Game) requestAcceptParty(u *User, pkt protocol.Packet) {
	var p wire.PartyAccept
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	leaderID := int(p.LeaderID)
	if leaderID <= 0 || leaderID >= MaxPlayer || leaderID == u.id || g.users[leaderID].status != userPlay {
		return
	}
	if u.partyInvite != leaderID || time.Since(u.partyInviteAt) > inviteTimeout {
		return // no invite from that leader
	}
	u.partyInvite = 0
	g.joinParty(u, leaderID)
}

// joinParty is the joining part of RequestAcceptParty (also reached by the
// group finder's password join), with its group finder bookkeeping.
func (g *Game) joinParty(u *User, leaderID int) {
	if leaderID <= 0 || leaderID >= MaxPlayer || leaderID == u.id || g.users[leaderID].status != userPlay {
		return
	}
	leader, me := &g.mobs[leaderID], &g.mobs[u.id]
	if leader.npc.leader != 0 {
		g.clientMessage(u, "O usuario não é líder do grupo.")
		return
	}
	if me.npc.leader != 0 || g.isLeaderWithParty(u.id) {
		g.clientMessage(u, "Saia do grupo para criar um.")
		return
	}
	slot := -1
	for i, f := range leader.npc.followers {
		if f == 0 {
			slot = i
			break
		}
	}
	if slot == -1 {
		g.clientMessage(u, "Grupo esta cheio.")
		return
	}
	if t := me.target; t.X >= 2608 && t.X <= 2647 && t.Y >= 1708 && t.Y <= 1748 {
		return // guild war arena
	}
	if !g.isLeaderWithParty(leaderID) { // a new party: the leader sees itself
		g.sendAddParty(leaderID, leaderID, true)
	}
	// The new member always gets its own line (the C++ sent it through the
	// member loop; the first port only did it for the first member, so
	// later members did not see themselves in the party window).
	g.sendAddParty(u.id, u.id, false)
	me.npc.leader = leaderID
	leader.npc.followers[slot] = u.id
	g.sendAddParty(u.id, leaderID, true)
	g.sendAddParty(leaderID, u.id, false)
	for _, id := range leader.npc.followers {
		if id <= 0 || id >= MaxPlayer || id == u.id {
			continue
		}
		g.sendAddParty(id, u.id, false)
		g.sendAddParty(u.id, id, false)
	}

	// Group finder: the member's own listing goes and, if the leader has a
	// password, the member learns it. Unlike the C++, forming a party does
	// not create one: the leader sets it with /passgrupo, /definirsenha or
	// /group atived.
	g.removeGroup(g.charName(u.id))
	g.sendGroupList(u, 1)
	u.partyPass = ""
	if lp := g.users[leaderID].partyPass; usablePass(lp) {
		u.partyPass = lp
	}
	g.sendGroupInfo(leaderID)
	for _, id := range leader.npc.followers {
		if id > 0 && id < MaxPlayer {
			g.sendGroupInfo(id)
		}
	}
}

// requestExitParty is RequestExitParty: 0 = leave, else the leader kicks.
func (g *Game) requestExitParty(u *User, pkt protocol.Packet) {
	var p wire.PartyExit
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	target := int(p.MobID)
	if target < 0 || target >= MaxPlayer {
		return
	}
	g.sendPartyEvocationFlag(u.id, 0) // SendPartyEvocation(clientId, 0)
	if target == 0 || target == u.id {
		g.removeParty(u.id)
		return
	}
	if g.mobs[u.id].npc.leader != 0 || g.mobs[target].npc.leader != u.id {
		g.clientMessage(u, "Somente o líder do grupo pode fazer isso.")
		return
	}
	g.removeParty(target)
}

// removeParty is RemoveParty: a member leaves, or a leader leaves and the
// first member takes over (or the party ends when fewer than 2 remain).
func (g *Game) removeParty(id int) {
	if id >= MaxPlayer {
		g.removeNPCParty(id)
		return
	}
	m := &g.mobs[id]
	leaderID := m.npc.leader
	if leaderID != 0 && g.users[leaderID].status != userPlay {
		m.npc.leader = 0
		return
	}
	g.sendRemoveParty(id, 0)
	g.users[id].follow = 0
	if leaderID != 0 { // a member leaves
		g.sendRemoveParty(leaderID, id)
		g.removeGroup(g.charName(id))
		m.npc.leader = 0
		for i, f := range g.mobs[leaderID].npc.followers {
			if f == 0 {
				continue
			}
			if f == id {
				g.mobs[leaderID].npc.followers[i] = 0
				continue
			}
			g.sendRemoveParty(f, id)
		}
		// Fixed: the C++ left the leader in a party of one (its own line
		// still in the window); with nobody left the party ends.
		lm := &g.mobs[leaderID]
		left := false
		for _, f := range lm.npc.followers {
			if f > 0 && f < MaxPlayer && g.users[f].status == userPlay && g.mobs[f].npc.leader == leaderID {
				left = true
			}
		}
		if !left {
			lm.npc.followers = [maxParty]int{}
			g.users[leaderID].follow = 0
			g.sendRemoveParty(leaderID, 0)
			g.removeGroup(g.charName(leaderID))
		}
		return
	}
	// The leader leaves.
	members := 0
	newLeader := 0
	for _, f := range m.npc.followers {
		if f > 0 && f < MaxPlayer && g.users[f].status == userPlay {
			members++
			if newLeader == 0 {
				newLeader = f
			}
		}
	}
	if newLeader != 0 {
		g.signalParm(g.users[newLeader], uint16(newLeader), opGroupFail, 0)
	}
	g.removeGroup(g.charName(id))
	if newLeader != 0 && members >= 2 && len(g.users[id].partyPass) > 0 {
		for _, f := range m.npc.followers {
			if f > 0 && f < MaxPlayer {
				g.users[f].partyPass = g.users[id].partyPass
				g.clientMessage(g.users[f], "Senha do grupo recuperada %s", g.users[f].partyPass)
			}
		}
	}
	if newLeader == 0 || members < 2 {
		for i, f := range m.npc.followers {
			if f == 0 {
				continue
			}
			m.npc.followers[i] = 0
			g.mobs[f].npc.leader = 0
			g.sendRemoveParty(f, 0)
		}
		return
	}
	nl := &g.mobs[newLeader]
	nl.npc.leader = 0
	nl.npc.followers = [maxParty]int{}
	n := 0
	for _, f := range m.npc.followers {
		if f != 0 && f != newLeader {
			nl.npc.followers[n] = f
			n++
		}
	}
	g.sendRemoveParty(newLeader, id)
	g.sendAddParty(newLeader, newLeader, true)
	for _, f := range nl.npc.followers {
		if f == 0 {
			continue
		}
		g.mobs[f].npc.leader = newLeader
		g.sendRemoveParty(f, id)
		g.sendAddParty(f, newLeader, true)
	}
	m.npc.followers = [maxParty]int{}
}

// partyMembers returns the leader and members of id's party (just id when
// solo).
func (g *Game) partyMembers(id int) []int {
	leader := id
	if l := g.mobs[id].npc.leader; l != 0 {
		leader = l
	}
	out := []int{leader}
	for _, f := range g.mobs[leader].npc.followers {
		if f > 0 && f < MaxPlayer {
			out = append(out, f)
		}
	}
	return out
}
