package game

import (
	"encoding/binary"
	"testing"

	"dbsrv/internal/wire"
)

func invite(g *Game, from, to int) {
	p := wire.PartyInvite{Header: wire.NewHeader(wire.OpCliPartyInvite, uint16(from)), LeaderID: uint16(from), TargetID: uint16(to)}
	g.requestAddParty(g.users[from], packet(&p))
}

func accept(g *Game, who, leader int) {
	p := wire.PartyAccept{Header: wire.NewHeader(wire.OpCliPartyAccept, uint16(who)), LeaderID: uint16(leader)}
	g.requestAcceptParty(g.users[who], packet(&p))
}

func exitParty(g *Game, who, target int) {
	p := wire.PartyExit{Header: wire.NewHeader(wire.OpCliPartyExit, uint16(who)), MobID: int16(target)}
	g.requestExitParty(g.users[who], packet(&p))
}

// partyGame has players 1..4 in play.
func partyGame(t *testing.T) *Game {
	g, _, _ := tradeGame(t)
	for _, id := range []int{3, 4} {
		g.users[id].status = userPlay
		g.mobs[id].mode = 1
		g.mobs[id].info.Player.Status.CurHP = 100
	}
	return g
}

func TestPartyInviteAcceptLeave(t *testing.T) {
	g := partyGame(t)

	accept(g, 2, 1) // no invite yet
	if g.mobs[2].npc.leader != 0 {
		t.Fatal("joined without an invite")
	}
	// Inviting on someone else's behalf is ignored.
	p := wire.PartyInvite{LeaderID: 3, TargetID: 2}
	g.requestAddParty(g.users[1], packet(&p))
	accept(g, 2, 3)
	if g.mobs[2].npc.leader != 0 {
		t.Fatal("forged invite worked")
	}

	invite(g, 1, 2)
	accept(g, 2, 1)
	invite(g, 1, 3)
	accept(g, 3, 1)
	if g.mobs[2].npc.leader != 1 || g.mobs[3].npc.leader != 1 || g.mobs[1].npc.followers[0] != 2 || g.mobs[1].npc.followers[1] != 3 {
		t.Fatalf("party: %+v", g.mobs[1].npc.followers)
	}
	if got := g.partyMembers(3); len(got) != 3 || got[0] != 1 {
		t.Fatalf("members %v", got)
	}

	// A member cannot kick; player 4 (not in the party) cannot be kicked.
	exitParty(g, 2, 3)
	exitParty(g, 1, 4)
	if g.mobs[3].npc.leader != 1 {
		t.Fatal("non-leader kicked a member")
	}

	// The leader leaves: player 2 takes over with player 3.
	exitParty(g, 1, 0)
	if g.mobs[1].npc.followers != [12]int{} || g.mobs[2].npc.leader != 0 || g.mobs[3].npc.leader != 2 || g.mobs[2].npc.followers[0] != 3 {
		t.Fatalf("after leader left: 1=%+v 2=%+v 3=%+v", g.mobs[1].npc, g.mobs[2].npc, g.mobs[3].npc)
	}

	// The new leader kicks the last member: the party ends.
	exitParty(g, 2, 3)
	if g.mobs[3].npc.leader != 0 || g.isLeaderWithParty(2) {
		t.Fatal("party not ended")
	}
}

func TestPartyDisbandsWhenLeaderLeavesWithOneMember(t *testing.T) {
	g := partyGame(t)
	invite(g, 1, 2)
	accept(g, 2, 1)
	g.closeUser(g.users[1]) // disconnect
	if g.mobs[2].npc.leader != 0 || g.isLeaderWithParty(2) {
		t.Fatalf("member still in a party: %+v", g.mobs[2].npc)
	}
}

func TestPartySharesExp(t *testing.T) {
	g := worldGame(t)
	npc := 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		pl := &m.info.Player
		if m.mode != 0 && pl.Info&0x3F == 0 && pl.CapeInfo <= 2 && pl.Exp > 0 && pl.Equip[13].Index == 0 &&
			g.data.attribute(int(m.target.X), int(m.target.Y))&attrVillage == 0 {
			npc = id
			break
		}
	}
	mt := g.mobs[npc].target
	placePlayer(g, int(mt.X)+1, int(mt.Y), 0)
	// Player 2 next to player 1.
	u2 := g.users[2]
	u2.status = userPlay
	m2 := &g.mobs[2]
	m2.mode = 1
	m2.info.Player.Status.CurHP, m2.info.Player.Status.Level = 100, 10
	x, y := int(mt.X)+2, int(mt.Y)
	g.emptyMobGrid(2, &x, &y)
	m2.target = wire.Position{X: uint16(x), Y: uint16(y)}
	g.setCell(x, y, 2)
	g.mobs[1].info.Player.Status.Level = 10
	m2.info.Player.Equip[0].Effect[1].Value = evoMortal

	invite(g, 1, 2)
	accept(g, 2, 1)
	g.shareExp(func(int) int64 { return 1000 }, 1, g.mobs[1].target)
	e1, e2 := g.mobs[1].info.Player.Exp, m2.info.Player.Exp
	if e1 <= 0 || e2 <= 0 {
		t.Fatalf("exp: killer %d, member %d", e1, e2)
	}
	// Both members get the full share (base + 30), which GetExpApply_2 then
	// turns into a flat 1000 for Mortals outside the exp zones.
	if v := g.data.village(int(mt.X), int(mt.Y)); e1 != 1000 || e2 != 1000 {
		t.Fatalf("mortals outside the exp zones get 1000 each: %d, %d (village %d)", e1, e2, v)
	}

	// A member far away gets nothing.
	g.setCell(x, y, 0)
	m2.target = wire.Position{X: uint16(x + 100), Y: uint16(y)}
	g.shareExp(func(int) int64 { return 1000 }, 1, g.mobs[1].target)
	if m2.info.Player.Exp != e2 {
		t.Fatal("far member got exp")
	}
}

// The reported case: A invites B, B accepts, A leaves, B invites A.
// The client reads 0x37E as MSG_STANDARDPARM (an int): a 14-byte packet let
// the next packet's bytes into Parm, so "clear the party" (0) did not clear
// A's window. Inviting also sends the "evoc2" whisper, which must not answer
// "not connected".
func TestLeaderLeavesThenRejoins(t *testing.T) {
	g := worldGame(t)
	a := placePlayer(g, 2100, 2100, 0)
	b := secondPlayer(g)
	listen(a, b)
	invite(g, 1, 2)
	accept(g, 2, 1)
	a.out, b.out = nil, nil

	g.requestExitParty(a, packet(&wire.PartyExit{Header: wire.NewHeader(wire.OpCliPartyExit, 1)}))
	for _, u := range []*User{a, b} {
		cleared := false
		for _, raw := range sent(u, wire.OpCliPartyExit) {
			if len(raw) != 16 {
				t.Fatalf("0x37E is %d bytes, the client reads 16", len(raw))
			}
			if int32(binary.LittleEndian.Uint32(raw[12:16])) == 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Fatalf("player %d: party window not cleared", u.id)
		}
	}
	if g.mobs[1].npc.leader != 0 || g.mobs[2].npc.leader != 0 || g.isLeaderWithParty(1) || g.isLeaderWithParty(2) {
		t.Fatal("party still up on the server")
	}

	b.out = nil
	command(g, b, "evoc2", "")
	for _, m := range texts(b) {
		t.Fatalf("evoc2 answered %q", m)
	}
	invite(g, 2, 1)
	accept(g, 1, 2)
	if g.mobs[1].npc.leader != 2 {
		t.Fatal("A could not join B's party")
	}
}

// The reported case: A (high level) and B in a party. The client takes
// 0x338's Exp as its own exp when the killer is in its party, so each
// member must get its own exp there, never the killer's.
func TestKillPacketCarriesEachMembersExp(t *testing.T) {
	g := worldGame(t)
	npc := 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && m.info.Player.Info&0x3F == 0 && m.info.Player.CapeInfo <= 2 && m.info.Player.Exp > 0 {
			npc = id
			break
		}
	}
	mt := g.mobs[npc].target
	a := placePlayer(g, int(mt.X)+1, int(mt.Y), 0)
	b := secondPlayer(g)
	listen(a, b)
	invite(g, 1, 2)
	accept(g, 2, 1)
	pa, pb := &g.mobs[1].info.Player, &g.mobs[2].info.Player
	pa.Exp, pb.Exp = 900_000_000, 1_000_000
	pb.Equip[0].Effect[1].Value = evoMortal
	a.out, b.out = nil, nil
	g.mobKilled(npc, 1)
	check := func(u *User, want int64) {
		t.Helper()
		got := false
		for _, raw := range sent(u, wire.OpSrvKilled) {
			var k wire.Killed
			if wire.Unmarshal(raw, &k) == nil && int(k.Killer) == 1 {
				got = true
				if k.Exp != want {
					t.Fatalf("player %d got exp %d in the kill packet, has %d", u.id, k.Exp, want)
				}
			}
		}
		if !got {
			t.Fatalf("player %d got no kill packet", u.id)
		}
	}
	check(a, pa.Exp)
	check(b, pb.Exp)
	if pb.Exp <= 1_000_000 {
		t.Fatal("the member got no exp")
	}
}

// By request the base exp is scaled by each member's level: a capped
// killer (level 400) no longer leaves the member with only +30.
func TestPartyExpByMemberLevel(t *testing.T) {
	g := worldGame(t)
	npc := 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && m.info.Player.Info&0x3F == 0 && m.info.Player.CapeInfo <= 2 && m.info.Player.Exp > 1000 && m.info.Player.Status.Level > 50 {
			npc = id
			break
		}
	}
	if npc == 0 {
		t.Skip("no suitable mob")
	}
	mt := g.mobs[npc].target
	placePlayer(g, int(mt.X)+1, int(mt.Y), 0)
	b := secondPlayer(g)
	listen(g.users[1], b)
	invite(g, 1, 2)
	accept(g, 2, 1)
	pa, pb := &g.mobs[1].info.Player, &g.mobs[2].info.Player
	pa.Status.Level, pa.BStatus.Level = maxLevel, maxLevel // capped killer
	pa.Equip[0].Effect[1].Value = 0                        // no zone table: see the raw share
	pb.Status.Level, pb.BStatus.Level = 129, 129
	pb.Equip[0].Effect[1].Value = 0
	pb.Exp = nextLevel[evoMortal][128]
	if g.expApply(g.mobs[npc].info.Player.Exp, 1, npc) != 0 {
		t.Fatal("the capped killer should have no base exp")
	}
	want := g.expApply(g.mobs[npc].info.Player.Exp, 2, npc)
	before := pb.Exp
	g.mobKilled(npc, 1)
	if got := pb.Exp - before; got != want+30 {
		t.Fatalf("member got %d, want its own base %d + 30", got, want)
	}
}
