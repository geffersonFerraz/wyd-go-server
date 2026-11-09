package game

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The custom group finder (RequestListGrupo.cpp, RequestAcceptParty.cpp,
// the group commands of RequestCommand.cpp, GroupTransfer, StandingByFllw):
// leaders publish their party with a password; others list (0xCDB1) or
// search (0xCDB6) the groups and join with "/entrar lider senha". Leaders
// can let members auto-follow them ("/atvfll") and allow evocations
// ("/atvevoc").
//
// Differences from the C++ (Fixed):
//   - a password shorter than 4 characters ("0" = disabled by "/group
//     desatived") never lets anyone in;
//   - "/mgetin" looked the leader up by the command name ("mgetin"), so it
//     always failed; it uses the leader name it was given;
//   - "/countwater" sent the same line once per party member.

const (
	opGroupList   = 0xCDB1
	opGroupSearch = 0xCDB6
	opGroupInfo   = 0x99B
	opGroupFail   = 0x99C
	opReconnect   = 0x99F
	opClientInfo  = 0x2132
	opSummonCall  = 0x3B2
	groupsPerPage = 10
)

type groupEntry struct {
	leader    string
	pass      string
	clone     bool // members may follow the leader
	evocation bool // members may summon
	public    bool // Status: the password is shown in the list
	members   int
}

// GroupListEntry is one leader of MSG_ListaGrupo (40 bytes).
type GroupListEntry struct {
	Leader    [16]byte
	Pass      [6]byte
	_         [2]byte
	Clone     int32
	Evocation int32
	Status    int32
	Member    int32
}

// GroupList is MSG_ListaGrupo (424 bytes).
type GroupList struct {
	Header      wire.Header
	LeaderCount int32
	Leaders     [groupsPerPage]GroupListEntry
	Update      bool
	_           [3]byte
	SceneIndex  int32 // total pages
}

// GroupSearch is pMsgSignalChar (32 bytes).
type GroupSearch struct {
	Header wire.Header
	Value  int32
	Nick   [16]byte
}

// GroupInfo is MSG_ReconnectInfoGrupo (36 bytes).
type GroupInfo struct {
	Header   wire.Header
	Password [6]byte
	Leader   [16]byte
	_        [2]byte // sizeof = 36
}

// ClientInfo is stSendInfoClient (52 bytes).
type ClientInfo struct {
	Header        wire.Header
	ExpBonus      int32
	DropBonus     int32
	AbsDamage     int32
	PerfuDamage   int32
	Cash          int32
	Password      [12]byte
	UsersRegister int32
	Honra         int32
}

// SummonCall is p3B2 (32 bytes): "x wants to summon you".
type SummonCall struct {
	Header   wire.Header
	Unknown  int32
	Nickname [16]byte
}

// cmdN is strncmp(cmd, lit, n) == 0: lit's first n characters, counting
// its terminator, must match.
func cmdN(cmd, lit string, n int) bool {
	at := func(s string, i int) byte {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	for i := 0; i < n; i++ {
		a, b := at(cmd, i), at(lit, i)
		if a != b {
			return false
		}
		if a == 0 {
			return true
		}
	}
	return true
}

func (g *Game) findGroup(leader string) int {
	for i := range g.groups {
		if g.groups[i].leader == leader {
			return i
		}
	}
	return -1
}

// removeGroup is RemoveLeaderListGroup (kept while that leader reconnects).
func (g *Game) removeGroup(leader string) {
	if leader == "" || len(g.groups) == 0 {
		return
	}
	if id := g.userByName(leader); id > 0 && g.users[id].reconnect {
		return
	}
	if i := g.findGroup(leader); i >= 0 {
		g.groups = append(g.groups[:i], g.groups[i+1:]...)
	}
}

// listGroup is SetListGrupoSexy: a leader with a password and members is
// listed (members count only their own PartyList, which is empty, so they
// add nothing - as in the C++).
func (g *Game) listGroup(id int) {
	u := g.users[id]
	if u.status != userPlay || len(u.partyPass) < 2 || g.mobs[id].npc.leader != 0 {
		return
	}
	count := 0
	for _, f := range g.mobs[id].npc.followers {
		if f > 0 && f < MaxPlayer && g.users[f].status == userPlay && f != id {
			count++
		}
	}
	if count <= 0 {
		return
	}
	name := g.charName(id)
	if i := g.findGroup(name); i >= 0 {
		g.groups[i].members = count
		return
	}
	g.groups = append(g.groups, groupEntry{leader: name, members: count})
}

// requestGroupList is Msg_ListaGrupo (0xCDB1): Value = page.
func (g *Game) requestGroupList(u *User, pkt protocol.Packet) {
	var p wire.Parm1
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	g.sendGroupList(u, int(p.Parm1))
}

func (g *Game) sendGroupList(u *User, page int) {
	for i := 1; i < MaxPlayer; i++ {
		g.listGroup(i)
	}
	g.sendGroupPage(u, g.groups, page)
}

// requestGroupSearch is Msg_BuscaListaGrupo (0xCDB6): leaders whose name
// contains Nick (any case); one search per 700 ms.
func (g *Game) requestGroupSearch(u *User, pkt protocol.Packet) {
	var p GroupSearch
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if time.Since(u.lastGroupFind) < 700*time.Millisecond {
		return
	}
	u.lastGroupFind = time.Now()
	search := strings.ToLower(wire.DecodeGame(wire.CString(p.Nick[:])))
	var found []groupEntry
	for _, e := range g.groups {
		if strings.Contains(strings.ToLower(e.leader), search) {
			found = append(found, e)
		}
	}
	g.sendGroupPage(u, found, int(p.Value))
}

func (g *Game) sendGroupPage(u *User, list []groupEntry, page int) {
	total := len(list)
	pages := (total + groupsPerPage - 1) / groupsPerPage
	page = min(page, pages)
	page = max(page, 1)
	start := (page - 1) * groupsPerPage
	end := min(start+groupsPerPage, total)
	p := GroupList{Header: wire.NewHeader(opGroupList, uint16(u.id)), Update: true, SceneIndex: int32(pages)}
	n := 0
	for i := start; i < end; i++ {
		e := list[i]
		le := &p.Leaders[n]
		wire.PutGameString(le.Leader[:15], e.leader)
		if e.public {
			wire.PutCString(le.Pass[:5], e.pass)
		}
		le.Clone, le.Evocation, le.Status, le.Member = b2i(e.clone), b2i(e.evocation), b2i(e.public), int32(e.members)
		n++
	}
	p.LeaderCount = int32(n)
	g.queue(u, &p)
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// sendGroupInfo is SendReconnectInfogrupo (0x99B): the password and leader.
func (g *Game) sendGroupInfo(id int) {
	u := g.player(id)
	if u == nil || u.status != userPlay {
		return
	}
	leader := g.mobs[id].npc.leader
	if leader <= 0 {
		leader = id
	}
	p := GroupInfo{Header: wire.NewHeader(opGroupInfo, uint16(id))}
	wire.PutCString(p.Password[:5], u.partyPass)
	wire.PutCString(p.Leader[:15], wire.CString(g.mobs[leader].info.Player.Name[:]))
	g.queue(u, &p)
}

// sendClientInfo is SendClientPacket (0x2132): cash, honor and the party
// password for the client's panels.
func (g *Game) sendClientInfo(u *User) {
	if u == nil || u.status != userPlay {
		return
	}
	p := ClientInfo{Header: wire.NewHeader(opClientInfo, uint16(u.id)),
		Cash: int32(u.account.Cash), Honra: int32(u.account.Honra)}
	if slices.Contains(g.arena.registered, u.id) {
		p.UsersRegister = 1
	}
	wire.PutCString(p.Password[:7], u.partyPass)
	g.queue(u, &p)
}

// signalReconnect is SendSignalParmReconect: only for reconnecting clients.
func (g *Game) signalReconnect(u *User, op uint16) {
	if u.reconnect {
		g.signalParm(u, uint16(u.id), op, 0)
	}
}

func groupPass(lo, hi int) string { return strconv.Itoa(lo + rnd()%(hi-lo+1)) }

// usablePass reports whether a password can open a party.
func usablePass(p string) bool { return len(p) >= 4 }

// groupCommand runs the group finder commands; false when cmd is not one.
func (g *Game) groupCommand(u *User, cmd string, p *wire.Command) bool {
	value := wire.DecodeGame(wire.CString(p.Value[:]))
	switch {
	case cmdN(cmd, "passgrupo", 11):
		g.passGroup(u)
	case cmdN(cmd, "atvgrupo", 10):
		g.toggleGroup(u, func(e *groupEntry) {
			e.public = !e.public
			e.pass = u.partyPass
			if e.public {
				g.clientMessage(u, "Grupo definido como livre!")
			} else {
				g.clientMessage(u, "Grupo definido como privado!")
			}
		})
	case cmdN(cmd, "atvevoc", 9):
		g.toggleGroup(u, func(e *groupEntry) {
			e.evocation = !e.evocation
			if e.evocation {
				g.clientMessage(u, "Evocações definido como ativado!")
				return
			}
			for _, f := range g.mobs[u.id].npc.followers {
				if f >= 1 && f < MaxPlayer && g.users[f].status == userPlay {
					g.dismissSummons(f)
					g.clientMessage(g.users[f], "As invocacoes foram removidas pelo líder!")
				}
			}
			g.clientMessage(u, "As invocacoes foram removidas!")
		})
	case cmdN(cmd, "atvfll", 7):
		g.followCommand(u)
	case cmdN(cmd, "definirsenha", 5):
		u.partyPass = groupPass(1000, 9999)
		g.clientMessage(u, "Sua senha e: %s", u.partyPass)
	case cmdN(cmd, "entrar", 5):
		g.enterGroup(u, value)
	case cmdN(cmd, "transferirgrupo", 5):
		g.transferGroupCommand(u, value)
	case cmdN(cmd, "countwater", 10):
		leader := g.mobs[u.id].npc.leader
		if leader == 0 {
			leader = u.id
		}
		w := g.users[leader].waterCount
		g.clientMessage(u, "Water N: %d - Water M: %d - Water A: %d", w[0], w[1], w[2])
	case cmdN(cmd, "lgetin", 6):
		g.groupLoginCommand(u, p, false)
	case cmdN(cmd, "mgetin", 6):
		g.groupLoginCommand(u, p, true)
	case cmdN(cmd, "group", 5):
		switch value {
		case "atived":
			u.partyPass = groupPass(1000, 99999)
		case "desatived":
			u.partyPass = "0"
		default:
			return true
		}
		g.sendClientInfo(u)
		g.sendEtc(u)
	default:
		return false
	}
	return true
}

// passGroup is "/passgrupo": a new 4-digit password for the caller's group
// (members only drop their own listing).
func (g *Game) passGroup(u *User) {
	name := g.charName(u.id)
	if g.mobs[u.id].npc.leader != 0 {
		g.removeGroup(name)
		return
	}
	u.partyPass = groupPass(1000, 9999)
	var msg string
	if i := g.findGroup(name); i >= 0 {
		g.groups[i].pass = u.partyPass
		msg = "Sua senha foi atualizada para: " + u.partyPass
	} else {
		g.groups = append(g.groups, groupEntry{leader: name, pass: u.partyPass, members: 1})
		msg = "Seu novo grupo foi criado com a senha: " + u.partyPass
	}
	g.clientMessage(u, "%s", msg)
	g.chatTo(u, colorGreen, "[Grupo info] : "+msg)
}

// toggleGroup runs fn on the leader's listing (members drop their own).
func (g *Game) toggleGroup(u *User, fn func(*groupEntry)) {
	name := g.charName(u.id)
	if g.mobs[u.id].npc.leader != 0 {
		g.removeGroup(name)
		return
	}
	i := g.findGroup(name)
	if i < 0 {
		g.clientMessage(u, "Necessário gerar uma nova senha!")
		return
	}
	fn(&g.groups[i])
}

// followCommand is "/atvfll": a member toggles following the leader (if
// the leader allows it); the leader toggles whether members may follow.
func (g *Game) followCommand(u *User) {
	m := &g.mobs[u.id]
	if v := g.data.village(int(m.target.X), int(m.target.Y)); v >= 0 && v < 5 {
		g.clientMessage(u, "Somente fora das cidades.")
		return
	}
	if leader := m.npc.leader; leader != 0 {
		i := g.findGroup(g.charName(leader))
		if i < 0 {
			g.clientMessage(u, "Necessário gerar uma nova senha!")
			return
		}
		if !g.groups[i].clone {
			g.clientMessage(u, "Líder do grupo não permitiu!")
			return
		}
		if u.follow == 0 {
			u.follow = leader
			g.clientMessage(u, "Você agora está seguindo o líder!")
		} else {
			u.follow = 0
			g.clientMessage(u, "Você parou de seguir o líder.")
		}
		return
	}
	i := g.findGroup(g.charName(u.id))
	if i < 0 {
		g.clientMessage(u, "Necessário gerar uma nova senha!")
		return
	}
	e := &g.groups[i]
	e.clone = !e.clone
	if e.clone {
		g.clientMessage(u, "Agora, todos os membros podem te seguir!")
		return
	}
	g.clientMessage(u, "Agora, os membros não poderão mais te seguir.")
	for _, f := range m.npc.followers {
		if f >= 1 && f < MaxPlayer && g.users[f].status == userPlay {
			g.users[f].follow = 0
			g.clientMessage(g.users[f], "os membros não poderão mais seguir o líder!")
		}
	}
}

// evocationAllowed is the group finder check of the evocation skills.
func (g *Game) evocationAllowed(u *User) bool {
	leader := g.mobs[u.id].npc.leader
	if leader < 1 || leader >= MaxPlayer {
		return true
	}
	if i := g.findGroup(g.charName(leader)); i >= 0 && !g.groups[i].evocation {
		g.clientMessage(u, "Líder do grupo não permitiu!")
		return false
	}
	return true
}

// enterGroup is "/entrar lider senha".
func (g *Game) enterGroup(u *User, value string) {
	g.removeGroup(g.charName(u.id))
	f := strings.Fields(value)
	if len(f) < 2 {
		return
	}
	nick, pass := f[0], f[1]
	if len(nick) > 15 {
		nick = nick[:15]
	}
	if len(pass) > 5 {
		pass = pass[:5]
	}
	if len(nick) < 4 {
		g.clientMessage(u, "O nome deve conter de 4 ~ 16 caracteres.")
		return
	}
	leader := g.userByName(nick)
	if leader <= 0 {
		g.clientMessage(u, "O Jogador nao esta online.")
		return
	}
	if leader == u.id {
		return
	}
	if lp := g.users[leader].partyPass; !usablePass(lp) || lp != pass {
		g.clientMessage(u, "Senha incorreta.")
		return
	}
	if g.mobs[leader].npc.leader != 0 {
		g.clientMessage(u, "Usuário nao e lider de grupo.")
		return
	}
	g.joinParty(u, leader)
}

// groupLoginCommand is "/lgetin" (restore the password after a reconnect)
// and "/mgetin" (rejoin the leader's party): Value = leader, ValueNew =
// password.
func (g *Game) groupLoginCommand(u *User, p *wire.Command, join bool) {
	rawLeader, rawPass := wire.CString(p.Value[:]), wire.CString(p.ValueNew[:])
	if len(rawLeader) > 23 || len(rawPass) > 23 {
		return
	}
	lf, pf := strings.Fields(wire.DecodeGame(rawLeader)), strings.Fields(rawPass)
	if len(lf) == 0 || len(pf) == 0 {
		return
	}
	leaderName, pass := lf[0], pf[0]
	if len(leaderName) > 15 {
		leaderName = leaderName[:15]
	}
	if len(pass) > 5 {
		pass = pass[:5]
	}
	fail, failPass := uint16(opGroupFail), uint16(opGroupFail)
	if join {
		fail, failPass = 0x99C1, 0x99C2
	}
	leader := g.userByName(leaderName)
	if leader <= 0 {
		g.signalReconnect(u, fail)
		if !join {
			g.clientMessage(u, "O Jogador não está online.")
		}
		return
	}
	if !join {
		if len(pass) <= 2 {
			u.partyPass = groupPass(1000, 99999)
			g.clientMessage(u, "Senha gerada [%s]", u.partyPass)
		} else {
			u.partyPass = pass
			g.clientMessage(u, "Senha recuperada [%s]", u.partyPass)
		}
		if g.charName(leader) != leaderName {
			return
		}
		g.sendGroupInfo(u.id)
		g.sendClientInfo(u)
		g.sendEtc(u)
		return
	}
	if g.mobs[leader].npc.leader != 0 {
		g.signalReconnect(u, failPass)
		g.clientMessage(u, "Usuário não é líder de grupo %d.", g.mobs[leader].npc.leader)
		return
	}
	if lp := g.users[leader].partyPass; !usablePass(lp) || lp != pass {
		g.signalReconnect(u, failPass)
		return
	}
	g.joinParty(u, leader)
}

// transferGroupCommand is "/transferirgrupo nick".
func (g *Game) transferGroupCommand(u *User, nick string) {
	to := g.userByName(strings.TrimSpace(nick))
	if to <= 0 {
		g.clientMessage(u, "O usuario nao esta conectado.")
		return
	}
	if g.mobs[u.id].npc.leader != 0 {
		g.clientMessage(u, "%s", g.lang(nnPartyLeaderOnly))
		return
	}
	member := false
	for _, f := range g.mobs[u.id].npc.followers {
		if f == to {
			member = true
		}
	}
	if !member {
		g.clientMessage(u, "O membro deve ser do seu grupo")
		return
	}
	g.groupTransfer(u.id, to)
}

// groupTransfer is GroupTransfer: to becomes the leader, from a member.
func (g *Game) groupTransfer(from, to int) {
	g.pistaTransfer(from, to)
	old, nl := &g.mobs[from], &g.mobs[to]
	nl.npc.followers = old.npc.followers
	for i, f := range nl.npc.followers {
		if f == to {
			nl.npc.followers[i] = from
			break
		}
	}
	g.users[to].partyPass = g.users[from].partyPass
	g.clientMessage(g.users[to], "Senha do grupo recuperada %s", g.users[to].partyPass)
	old.npc.followers = [maxParty]int{}
	nl.npc.leader = 0
	old.npc.leader = to
	g.sendAddParty(to, to, true)
	g.sendAddParty(from, from, false)
	g.sendAddParty(from, to, true)
	g.sendAddParty(to, from, false)
	for _, f := range nl.npc.followers {
		if f <= 0 || f >= MaxMob {
			continue
		}
		g.mobs[f].npc.leader = to
		g.sendAddParty(f, from, false)
		g.sendAddParty(f, to, true)
	}
}

// followTimer is the auto-follow part of ProcessSecTimer: outside the
// towns a member following its leader walks after it (teleports beyond
// 30 cells).
func (g *Game) followTimer() {
	for i := 1; i < MaxPlayer; i++ {
		u := g.users[i]
		m := &g.mobs[i]
		leader := m.npc.leader
		if u.status != userPlay || leader < 1 || leader >= MaxPlayer || u.follow != leader {
			continue
		}
		lm := &g.mobs[leader]
		if g.users[leader].status != userPlay || m.info.Player.Status.CurHP <= 0 || lm.info.Player.Status.CurHP <= 0 {
			continue
		}
		if v := g.data.village(int(m.target.X), int(m.target.Y)); v >= 0 && v < 5 {
			continue
		}
		if v := g.data.village(int(lm.target.X), int(lm.target.Y)); v >= 0 && v < 5 {
			continue
		}
		lx, ly := int(lm.target.X), int(lm.target.Y)
		d := distance(int(m.target.X), int(m.target.Y), lx, ly)
		switch {
		case d >= 30:
			g.teleportar(i, lx, ly)
		case d > 2:
			x, y, ok := g.routeTo(i, lx, ly, 3, false)
			if !ok || (x == int(m.target.X) && y == int(m.target.Y)) {
				continue
			}
			p := g.action(i, x, y)
			p.MoveSpeed = uint32(moveSpeed(&m.info.Player.Status))
			g.queue(u, &p)
			g.multicastMove(i, x, y, &p)
		}
	}
}

// requestReconnectFlag is 0x99F: the client says it is reconnecting.
func (g *Game) requestReconnectFlag(u *User, pkt protocol.Packet) {
	var p wire.Parm1
	if wire.Unmarshal(pkt.Raw, &p) == nil {
		u.reconnect = p.Parm1 != 0
	}
}
