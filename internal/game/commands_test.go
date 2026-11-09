package game

import (
	"encoding/binary"
	"strings"
	"testing"

	"dbsrv/internal/wire"
)

func TestCommandPacketSizes(t *testing.T) {
	for _, c := range []struct {
		v    any
		size int
	}{
		{DailyRewardInfo{}, 416}, {MissionInfo{}, 432}, {MissionTimes{}, 524}, {RollInfo{}, 172},
		{GroupListEntry{}, 40}, {GroupList{}, 424}, {GroupSearch{}, 32}, {GroupInfo{}, 36},
		{ClientInfo{}, 52}, {SummonCall{}, 32},
	} {
		if got := binary.Size(c.v); got != c.size {
			t.Errorf("%T: %d bytes, want %d", c.v, got, c.size)
		}
	}
}

func TestCmdN(t *testing.T) {
	for _, c := range []struct {
		cmd, lit string
		n        int
		want     bool
	}{
		{"passgrupo", "passgrupo", 11, true}, {"passgrupox", "passgrupo", 11, false},
		{"entra", "entrar", 5, true}, {"entrarxyz", "entrar", 5, true}, {"entr", "entrar", 5, false},
		{"group", "group", 5, true}, {"groupie", "group", 5, true}, {"lgetin", "lgetin", 6, true},
	} {
		if got := cmdN(c.cmd, c.lit, c.n); got != c.want {
			t.Errorf("cmdN(%q, %q, %d) = %v", c.cmd, c.lit, c.n, got)
		}
	}
}

// texts returns (and clears) the text messages queued for u.
func texts(u *User) []string {
	var out []string
	for _, b := range sent(u, wire.OpSrvMessage) {
		var m wire.Message128
		if wire.Unmarshal(b, &m) == nil {
			out = append(out, wire.GameString(m.Msg[:]))
		}
	}
	return out
}

func TestNoCommandLeftUnported(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	for _, cmd := range []string{"daily", "roleta", "summonguild", "spk", "gritar", "passe", "gm",
		"attpass", "attrank", "novato", "honra", "quest", "evoc", "sagua", "sarmia", "sgelo", "serion",
		"sazran", "skefra", "spilar", "passgrupo", "atvgrupo", "atvevoc", "atvfll", "definirsenha",
		"entrar", "transferirgrupo", "tab", "nig", "snd", "countwater", "lgetin", "mgetin", "group",
		"relo", "registerbattle", "summon", "srv507"} {
		u.out = nil
		command(g, u, cmd, "")
		for _, m := range texts(u) {
			if strings.Contains(m, "dispon") {
				t.Errorf("/%s answered %q", cmd, m)
			}
		}
	}
	// The level backdoor is gone.
	g.mobs[1].info.Player.BStatus.Level = 10
	command(g, u, "@9638", "")
	if g.mobs[1].info.Player.BStatus.Level != 10 {
		t.Fatal("/@9638 changed the level")
	}
}

// /gm is a message to the staff: it never runs a command.
func TestGMIsAMessage(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 100)
	listen(u)
	u.accessLevel = 1
	pl := &g.mobs[1].info.Player
	command(g, u, "gm", "+coin 5")
	if pl.Gold != 100 {
		t.Fatal("/gm ran a command")
	}
	if m := texts(u); len(m) != 1 || !strings.Contains(m[0], "enviada") {
		t.Fatalf("answer: %v", m)
	}
	u.out = nil
	command(g, u, "gm", "de novo")
	if m := texts(u); len(m) != 1 || !strings.Contains(m[0], "Aguarde") {
		t.Fatalf("no cooldown: %v", m)
	}
}

func TestPanelCommands(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 100)
	listen(u)
	pl := &g.mobs[1].info.Player
	if out := g.adminCommand(u, "+coin 5"); pl.Gold != 5 || len(out) != 1 {
		t.Fatalf("+coin: gold %d, %v", pl.Gold, out)
	}
	if g.adminCommand(u, "+exp 12345"); pl.Exp != 12345 {
		t.Fatalf("+exp with an argument: %d", pl.Exp)
	}
	if g.adminCommand(u, "+item 412 0 0 0 0 0 0"); firstSlotOf(pl, 412) < 0 {
		t.Fatal("+item gave nothing")
	}
	if g.adminCommand(u, "+learn 33"); pl.Learn[1]&2 == 0 {
		t.Fatal("+learn 33 did not set Learn[1] bit 1")
	}
	if out := g.adminCommand(u, "+name Novo"); len(out) == 0 || !strings.Contains(out[0], "suportado") {
		t.Fatalf("unsupported command answer: %v", out)
	}
	if out := g.adminCommand(g.users[0], "+coin 5"); len(out) != 1 || !strings.Contains(out[0], "personagem") {
		t.Fatalf("no target: %v", out)
	}
	g.war.weekMode = weekCollect
	if g.adminCommand(g.users[0], "+weekmode 2"); g.war.weekMode != weekWarning || g.war.forceWeek != weekTeleported {
		t.Fatalf("+weekmode: %d %d", g.war.weekMode, g.war.forceWeek)
	}
	if g.adminCommand(g.users[0], "+drop 800"); g.dropAll != 800 {
		t.Fatalf("+drop: %d", g.dropAll)
	}
}

func TestGroupFinderPasswordJoin(t *testing.T) {
	g := worldGame(t)
	leader := placePlayer(g, 2100, 2100, 0)
	member := secondPlayer(g)
	listen(leader, member)
	copy(g.mobs[1].info.Player.Name[:], "Lider")

	command(g, leader, "passgrupo", "")
	i := g.findGroup("Lider")
	if i < 0 || !usablePass(leader.partyPass) || g.groups[i].pass != leader.partyPass {
		t.Fatalf("passgrupo: %+v pass %q", g.groups, leader.partyPass)
	}
	command(g, member, "entrar", "Lider 0000")
	if g.mobs[2].npc.leader != 0 {
		t.Fatal("joined with a wrong password")
	}
	command(g, member, "entrar", "Lider "+leader.partyPass)
	if g.mobs[2].npc.leader != 1 || g.mobs[1].npc.followers[0] != 2 {
		t.Fatal("password join failed")
	}
	if member.partyPass != leader.partyPass {
		t.Fatal("member did not learn the password")
	}

	// A disabled password ("0") opens nothing.
	g.removeParty(2)
	command(g, leader, "group", "desatived")
	command(g, member, "entrar", "Lider 0")
	if g.mobs[2].npc.leader != 0 {
		t.Fatal("joined with the disabled password")
	}
}

func TestGroupListAndFollowToggle(t *testing.T) {
	g := worldGame(t)
	leader := placePlayer(g, 2100, 2100, 0)
	member := secondPlayer(g)
	listen(leader, member)
	copy(g.mobs[1].info.Player.Name[:], "Lider")
	g.joinParty(member, 1)
	if leader.partyPass != "" || member.partyPass != "" {
		t.Fatalf("forming a party made a password: %q / %q", leader.partyPass, member.partyPass)
	}
	for _, m := range texts(leader) {
		if strings.Contains(m, "Senha gerada") {
			t.Fatal("leader told about a generated password")
		}
	}
	// No password, no listing; the leader turns it on.
	member.out = nil
	g.sendGroupList(member, 1)
	var empty GroupList
	if pages := sent(member, opGroupList); len(pages) != 1 || wire.Unmarshal(pages[0], &empty) != nil || empty.LeaderCount != 0 {
		t.Fatal("a party without password was listed")
	}
	command(g, leader, "group", "atived")
	if !usablePass(leader.partyPass) {
		t.Fatalf("/group atived made no password: %q", leader.partyPass)
	}
	member.out = nil
	g.sendGroupList(member, 1)
	pages := sent(member, opGroupList)
	if len(pages) != 1 {
		t.Fatal("no group list")
	}
	var p GroupList
	if wire.Unmarshal(pages[0], &p) != nil || p.LeaderCount != 1 || wire.CString(p.Leaders[0].Leader[:]) != "Lider" || p.Leaders[0].Member != 1 {
		t.Fatalf("group list: %+v", p.Leaders[0])
	}
	if p.Leaders[0].Pass[0] != 0 {
		t.Fatal("private group shows its password")
	}
	// Listed groups start without evocations; the leader allows them.
	if g.evocationAllowed(member) {
		t.Fatal("evocations allowed before /atvevoc")
	}
	command(g, leader, "atvevoc", "")
	if !g.evocationAllowed(member) {
		t.Fatal("/atvevoc did not allow evocations")
	}
}

func TestSNDAndRelo(t *testing.T) {
	g := worldGame(t)
	a := placePlayer(g, 2100, 2100, 0)
	b := secondPlayer(g)
	listen(a, b)
	copy(g.mobs[1].info.Player.Name[:], "Primeiro")

	command(g, b, "snd", "fui almocar")
	a.out = nil
	command(g, a, "Segundo", "oi")
	found := false
	for _, m := range texts(a) {
		if strings.Contains(m, "fui almocar") {
			found = true
		}
	}
	if !found {
		t.Fatal("whisper did not show the /snd message")
	}

	// /relo needs the target's /summon (ClassInfo != 1).
	pb := &g.mobs[2].info.Player
	pb.ClassInfo = 0
	pb.Inventory[5] = wire.Item{Index: 699}
	g.mobs[1].info.Player.ClassInfo = 1
	g.mobs[1].info.Player.Learn[0] |= 0x40000
	g.teleportar(1, 2480, 1700)
	command(g, b, "relo", "Primeiro")
	if pb.Inventory[5].Index != 699 {
		t.Fatal("relo without a summon")
	}
	g.joinParty(b, 1)
	command(g, a, "summon", "Segundo")
	if a.summonedUser != 2 || len(sent(b, opSummonCall)) != 1 {
		t.Fatal("summon request not sent")
	}
	command(g, b, "relo", "Primeiro")
	if pb.Inventory[5].Index == 699 || a.summonedUser != 0 {
		t.Fatalf("relo after summon: scroll %d, pending %d", pb.Inventory[5].Index, a.summonedUser)
	}
}

func TestDailyRewardOncePerDay(t *testing.T) {
	g := worldGame(t)
	g.cfg.CacheDir = t.TempDir()
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	wire.PutCString(u.account.Username[:], "TESTE")
	pl := &g.mobs[1].info.Player
	g.requestCollectDaily(u, packet(&wire.Parm1{Header: wire.NewHeader(opDailyRewardCollect, 1)}))
	got := 0
	for _, it := range pl.Inventory[:30] {
		if it.Index != 0 {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("daily gave %d items", got)
	}
	g.requestCollectDaily(u, packet(&wire.Parm1{Header: wire.NewHeader(opDailyRewardCollect, 1)}))
	again := 0
	for _, it := range pl.Inventory[:30] {
		if it.Index != 0 {
			again++
		}
	}
	if again != 2 {
		t.Fatal("daily paid twice the same day")
	}
	// The mark survives in the shared cache file.
	u.account.Daily = wire.Daily{}
	g.loadDaily(u)
	if u.account.Daily.WeekYear == 0 {
		t.Fatal("daily state not saved to Cache/Daily")
	}
}

func TestMissionsAndRouletteData(t *testing.T) {
	g := worldGame(t)
	if len(g.data.Missions) == 0 {
		t.Skip("no Missions.xml")
	}
	m := g.data.Missions[0]
	if m.ID != 1 || m.MobName[0] != "Gremlin" || m.MobAmount[0] != 3 || m.Gold != 1100000 || m.Free[0].Index != 3322 {
		t.Fatalf("mission 1: %+v", m)
	}
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	g.mobs[1].info.DailyQuest.QuestID = 1
	g.sendMissionInfo(u)
	if len(sent(u, opMissionInfo)) != 1 {
		t.Fatal("no mission info")
	}
	if !g.data.Roulette[0].Status || g.data.Roulette[0].Roll[0].Index != 475 || g.data.Roulette[0].ShowSlots[0] != 0 {
		t.Fatalf("roulette box 1: %+v", g.data.Roulette[0].Roll[0])
	}
}
