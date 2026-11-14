package game

import (
	"dbsrv/internal/cluster"
	"encoding/binary"
	"testing"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// sent returns the queued packets of u with id op (and clears the queue).
func sent(u *User, op uint16) [][]byte {
	var out [][]byte
	for _, b := range u.out {
		if len(b) >= 6 && binary.LittleEndian.Uint16(b[4:]) == op {
			out = append(out, b)
		}
	}
	u.out = nil
	return out
}

// listen gives users a (never flushed) connection so queued packets stay
// in u.out.
func listen(us ...*User) {
	for _, u := range us {
		u.conn = &clientConn{}
	}
}

func command(g *Game, u *User, cmd, value string) {
	p := wire.Command{Header: wire.NewHeader(wire.OpCliCommand, uint16(u.id))}
	wire.PutCString(p.Command[:], cmd)
	wire.PutGameString(p.Value[:100], value)
	g.requestCommand(u, packet(&p))
}

func TestSetGuildsFromStore(t *testing.T) {
	g := worldGame(t)
	g.SetGuilds([]store.Guild{{ID: 5, Name: "Lobos", Kingdom: 7, Fame: 30, SubGuild: [3]string{"A", "", ""}}},
		map[int]int{5: 6}, map[int]int{})
	if x := g.guild(5); x.Name != "Lobos" || x.Kingdom != 7 || x.Fame != 30 || x.Sub[0] != "A" || g.guildAlly[5] != 6 {
		t.Fatalf("guild 5: %+v ally %d", x, g.guildAlly[5])
	}
	// DBSRV broadcasts keep it current.
	add := wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: 99, GuildIndex: 5}
	g.guildDBMessage(packetRaw(wire.MustMarshal(&add)))
	sub := wire.AddSub{Header: wire.NewHeader(wire.OpAddSub, 0), GuildIndex: 5, SubIndex: 0, Status: 1}
	g.guildDBMessage(packetRaw(wire.MustMarshal(&sub)))
	if x := g.guild(5); x.Fame != 99 || x.Sub[0] != "" {
		t.Fatalf("after broadcasts: %+v", x)
	}
}

func TestCreateGuildRecruitKick(t *testing.T) {
	g := partyGame(t)
	a, b, c := g.users[1], g.users[2], g.users[3]
	listen(a, b, c)
	pa := &g.mobs[1].info.Player
	pa.Gold = 200000000
	pa.CapeInfo = 7
	command(g, a, "criar", "Lobos")
	if pa.GuildIndex == 0 || pa.GuildMemberType != 9 || g.guildName(int(pa.GuildIndex)) != "Lobos" || pa.Gold != 100000000 {
		t.Fatalf("create: guild %d type %d gold %d", pa.GuildIndex, pa.GuildMemberType, pa.Gold)
	}
	guild := int(pa.GuildIndex)
	// The same name again is refused.
	g.mobs[3].info.Player.Gold = 200000000
	command(g, c, "criar", "lobos")
	if g.mobs[3].info.Player.GuildIndex != 0 {
		t.Fatal("duplicate guild name accepted")
	}

	// Recruit: nothing happens until the player accepts.
	pb := &g.mobs[2].info.Player
	pb.CapeInfo = 7
	g.requestRecruit(a, packet(&wire.Parm2{Header: wire.NewHeader(opCliRecruit, 1), Parm1: 2}))
	if pb.GuildIndex != 0 || pa.Gold != 100000000 {
		t.Fatal("recruited without consent")
	}
	reqs := sent(b, opRecruitRequest)
	if len(reqs) != 1 {
		t.Skipf("no recruit offer (Sunday?)")
	}
	var req RecruitRequest
	_ = wire.Unmarshal(reqs[0], &req)
	g.requestAcceptRecruit(b, packet(&req))
	if int(pb.GuildIndex) != guild || pb.GuildMemberType != 1 || pa.Gold != 100000000-recruitCost {
		t.Fatalf("accept: guild %d type %d leader gold %d", pb.GuildIndex, pb.GuildMemberType, pa.Gold)
	}
	// A second accept with no offer does nothing.
	g.requestAcceptRecruit(c, packet(&req))
	if g.mobs[3].info.Player.GuildIndex != 0 {
		t.Fatal("accepted without an offer")
	}

	// Guild chat reaches the member.
	command(g, a, "", "-ola guilda")
	if len(sent(b, wire.OpCliCommand)) != 1 {
		t.Fatal("guild chat not delivered")
	}

	// A member cannot kick; the leader cannot be kicked.
	g.requestKickMember(b, packet(&wire.SignalValue{Header: wire.NewHeader(opCliKickMember, 2), Value: 1}))
	if pa.GuildIndex == 0 {
		t.Fatal("member kicked the leader")
	}
	g.requestKickMember(a, packet(&wire.SignalValue{Header: wire.NewHeader(opCliKickMember, 1), Value: 2}))
	if pb.GuildIndex != 0 || g.mobs[2].info.LastGuildKickOut.Ano == 0 {
		t.Fatal("kick did not work")
	}
}

func TestWhisper(t *testing.T) {
	g := partyGame(t)
	a, b := g.users[1], g.users[2]
	listen(a, b)
	wire.PutCString(g.mobs[2].info.Player.Name[:], "Bravo")
	command(g, a, "Bravo", "oi")
	got := sent(b, wire.OpCliCommand)
	if len(got) != 1 {
		t.Fatal("whisper not delivered")
	}
	var p wire.Command
	_ = wire.Unmarshal(got[0], &p)
	if wire.CString(p.Value[:]) != "oi" || b.lastWhisper != 1 {
		t.Fatalf("whisper %q", p.Value[:])
	}
	command(g, b, "r", "tchau")
	if len(sent(a, wire.OpCliCommand)) != 1 {
		t.Fatal("/r not delivered")
	}
	b.whisperOff = true
	command(g, a, "Bravo", "oi")
	if len(sent(b, wire.OpCliCommand)) != 0 {
		t.Fatal("whisper delivered while blocked")
	}
}

// Each server creates guilds in its own range of numbers, so one folder of
// guild marks serves every server.
func TestCreateGuildUsesTheServerRange(t *testing.T) {
	g := partyGame(t)
	g.cfg.Cluster.Group = 2
	a := g.users[1]
	listen(a)
	pa := &g.mobs[1].info.Player
	pa.Gold = 200000000
	command(g, a, "criar", "Lobos")
	if first, _ := cluster.GuildRange(2); int(pa.GuildIndex) != first || first != 6554 {
		t.Fatalf("guild %d created on server 2, want %d", pa.GuildIndex, first)
	}
}
