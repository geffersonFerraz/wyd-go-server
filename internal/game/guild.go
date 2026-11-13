package game

import (
	"dbsrv/internal/cluster"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Guilds (g_pGuild, g_pGuildAlly, g_pGuildWar). The C++ TMSRV read
// ..\DBsrv\guilds.txt at startup; here the table comes from Postgres (the
// same guilds / guild_relations rows DBSRV writes, see SetGuilds). After
// that, every change goes through DBSRV, which saves it and rebroadcasts it
// to all channels (0x310 create, 0x313 fame/citizen/wins, 0x315 sub-leader,
// 0xE12 ally, 0xE0E war); this server applies the rebroadcasts.
//
// Member types (Player.GuildMemberType): 0 none, 1 member, 3..5 sub-leader
// (sub slot type-3), 9 leader.

const maxGuild = 65535 // MAX_GUILD

// Language.txt indexes (Basedef.h).
const (
	nnOnlyGuildMasterCan  = 24
	nnNotConnected        = 91
	nnOnlyGuildMemberCan  = 104
	nnOnlyGuildMasterCan2 = 144
	nnNoGuildMembers      = 162
	nnNeedGuildMedal      = 190
	snYouAreAtWar         = 218
	snWarToS              = 219
	snWarFromS            = 220
	snAllyToS             = 283
	snAllyFromS           = 284
	ssAllyCanceled        = 286
	ssAllyDeclared        = 287
	nnCantDeclareAlly     = 289
	nnNotEnoughGoldTele   = 297
	nnGuildNeed100mGold   = 327
	nnGuildYouHaveAGuild  = 328
	nnGuildLimit          = 329
	nnGuildGuildName      = 331
	nnGuildSuccessCreate  = 333
	nnGuildCantRecruitDay = 334
	nnGuildCantRecruitKgd = 336
	nnGuildCantRecruitGld = 338
	nnGuildRecruitEnter   = 340
	nnGuildRecruit        = 341
	nnGuildCantKick       = 342
	nnGuildKicked         = 343
	nnGuildSubCreated     = 345
	nnGuildSubCalled      = 346
	nnGuildMaxSub         = 358
	nnWhisperMP           = 353
	nnCPPoint             = 355
	nnNTAmount            = 356
	nnNTZero              = 357
)

const (
	opCliRecruit       = 0x3D5
	opRecruitRequest   = 0x680 // server -> invited player
	opCliRecruitAccept = 0x681
	opCliKickMember    = 0x28C
	opSrvWarInfo       = 0x3A8
	recruitCost        = 4000000
	createGuildCost    = 100000000
	subLeaderCost      = 50000000
	guildTicket        = 4614
	recruitTimeout     = 2 * time.Minute
)

// Guild is one g_pGuild entry. Names are UTF-8 (as in Postgres).
type Guild struct {
	Name    string
	Kingdom int
	Fame    int
	Citizen int
	Wins    int
	Sub     [3]string
}

// guildInvite is a recruit offer waiting for the invited player's answer.
type guildInvite struct {
	from  int // recruiter slot
	guild int
	at    time.Time
}

// SetGuilds loads the guild table (call before Run).
func (g *Game) SetGuilds(list []store.Guild, ally, war map[int]int) {
	g.guilds = map[int]*Guild{}
	for _, x := range list {
		if x.ID <= 0 || x.ID >= maxGuild {
			continue
		}
		g.guilds[x.ID] = &Guild{Name: x.Name, Kingdom: x.Kingdom, Fame: x.Fame, Citizen: x.Citizen, Wins: x.Wins, Sub: x.SubGuild}
	}
	g.guildAlly, g.guildWar = map[int]int{}, map[int]int{}
	for k, v := range ally {
		g.guildAlly[k] = v
	}
	for k, v := range war {
		g.guildWar[k] = v
	}
}

func (g *Game) guild(id int) *Guild {
	if x := g.guilds[id]; x != nil {
		return x
	}
	return &Guild{}
}

func (g *Game) guildName(id int) string { return g.guild(id).Name }

// guildDBMessage applies a DBSRV rebroadcast (GetNewGuild, GetGuildInfo,
// AddSubLider, DoAlly, war).
func (g *Game) guildDBMessage(pkt protocol.Packet) {
	switch pkt.Header.PacketID {
	case wire.OpCreateGuild:
		var p wire.CreateGuild
		if wire.Unmarshal(pkt.Raw, &p) != nil || p.GuildID <= 0 || p.GuildID >= maxGuild {
			return
		}
		g.guilds[int(p.GuildID)] = &Guild{Name: wire.GameString(p.GuildName[:]), Kingdom: int(p.Kingdom), Citizen: int(p.Citizen)}
	case wire.OpAddGuild:
		var p wire.AddGuild
		if wire.Unmarshal(pkt.Raw, &p) != nil || p.GuildIndex <= 0 || p.GuildIndex >= maxGuild {
			return
		}
		x := g.guildEntry(int(p.GuildIndex))
		switch p.Type {
		case 0:
			x.Fame = int(p.Value)
		case 1:
			x.Citizen = int(p.Value)
		case 2:
			x.Wins = int(p.Value)
		case guildInfoKingdom:
			if p.Value == capeBlue || p.Value == capeRed {
				x.Kingdom = int(p.Value)
				g.guildKingdomChanged(int(p.GuildIndex))
			}
		}
	case wire.OpAddSub:
		var p wire.AddSub
		if wire.Unmarshal(pkt.Raw, &p) != nil || p.GuildIndex <= 0 || p.SubIndex < 0 || p.SubIndex > 2 {
			return
		}
		x := g.guildEntry(int(p.GuildIndex))
		if p.Status == 1 {
			x.Sub[p.SubIndex] = ""
		} else {
			x.Sub[p.SubIndex] = wire.GameString(p.Name[:])
		}
	case wire.OpGuildAlly:
		var p wire.Parm3
		if wire.Unmarshal(pkt.Raw, &p) == nil {
			g.doAlly(int(p.Parm1), int(p.Parm2))
		}
	case wire.OpGuildWar:
		var p wire.Parm3
		if wire.Unmarshal(pkt.Raw, &p) == nil && p.Parm1 > 0 && p.Parm1 < maxGuild {
			g.guildWar[int(p.Parm1)] = int(p.Parm2)
		}
	}
}

// guildInfoKingdom is the AddGuild (0xC0x) type for a kingdom change (Go
// only: the guild leader took the guild to another kingdom at a king).
const guildInfoKingdom = 3

// guildKingdomChanged gives the online members with a kingdom cape the
// cape of the guild's new kingdom (the rest get it on entering the world,
// as before).
func (g *Game) guildKingdomChanged(guild int) {
	for _, u := range g.users[1:] {
		if u.status != userPlay {
			continue
		}
		pl := &g.mobs[u.id].info.Player
		if int(pl.GuildIndex) != guild || pl.CapeInfo == 0 {
			continue
		}
		before := pl.Equip[15]
		g.guildKingdomCape(pl)
		if pl.Equip[15] != before {
			g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
			g.refreshMob(u.id)
			g.clientMessage(u, "Sua guild mudou de reino.")
		}
	}
}

func (g *Game) guildEntry(id int) *Guild {
	x := g.guilds[id]
	if x == nil {
		x = &Guild{}
		g.guilds[id] = x
	}
	return x
}

// doAlly is DoAlly: ally 0 cancels the guild's alliance.
func (g *Game) doAlly(guild, ally int) {
	if guild <= 0 || ally < 0 || guild >= maxGuild || ally >= maxGuild || g.isWarTime() {
		return
	}
	current := g.guildAlly[guild]
	if current < 0 || current >= maxGuild {
		current = 0
	}
	switch {
	case ally == 0 && current > 0:
		msg := g.lang(ssAllyCanceled)
		g.guildNotice(guild, msg, g.guildName(guild), g.guildName(current))
		g.guildNotice(current, msg, g.guildName(guild), g.guildName(current))
		delete(g.guildAlly, guild)
	case current == 0 && ally != 0:
		msg := g.lang(ssAllyDeclared)
		g.guildNotice(guild, msg, g.guildName(guild), g.guildName(ally))
		g.guildNotice(ally, msg, g.guildName(guild), g.guildName(ally))
		g.guildAlly[guild] = ally
	}
	for _, u := range g.users[1:] {
		if u.status == userPlay && int(g.mobs[u.id].info.Player.GuildIndex) == guild {
			g.sendWarInfo(u)
		}
	}
}

// endAlliances cancels the guild's alliance and every alliance declared
// with it (Go only: the guild changed kingdom, and an alliance needs both
// guilds in the same kingdom). DBSRV saves and rebroadcasts each one.
func (g *Game) endAlliances(guild int) {
	for other, ally := range g.guildAlly {
		if (other == guild && ally > 0) || ally == guild {
			g.sendDB(&wire.GuildRelation{Header: wire.NewHeader(wire.OpGuildAlly, 0), GuildIndex1: uint32(other)})
		}
	}
}

// guildNotice is SendGuildNotice.
func (g *Game) guildNotice(guild int, format string, args ...any) {
	for _, u := range g.users[1:] {
		if u.status == userPlay && int(g.mobs[u.id].info.Player.GuildIndex) == guild {
			g.clientMessage(u, format, args...)
		}
	}
}

// sendWarInfo is SendWarInfo (0x3A8): enemy guild, cape that won, ally.
func (g *Game) sendWarInfo(u *User) {
	guild := int(g.mobs[u.id].info.Player.GuildIndex)
	war, ally := g.guildWar[guild], g.guildAlly[guild]
	if war < 0 || war >= maxGuild {
		war = 0
	}
	if ally < 0 || ally >= maxGuild {
		ally = 0
	}
	enemy := 0
	if guild != 0 && war != 0 && g.guildWar[war] != guild {
		enemy = war
	}
	g.queue(u, &wire.Parm3{Header: wire.NewHeader(opSrvWarInfo, wire.ClientIDSystem), Parm1: int32(enemy), Parm2: g.war.capeWin, Parm3: int32(ally)})
}

// sendGuildList is SendGuildList: members online, wars and alliances.
func (g *Game) sendGuildList(u *User) {
	guild := int(g.mobs[u.id].info.Player.GuildIndex)
	if guild <= 0 {
		return
	}
	line, count := "", 0
	for _, o := range g.users[1:] {
		if o.status != userPlay || int(g.mobs[o.id].info.Player.GuildIndex) != guild {
			continue
		}
		count++
		if len(line) >= 70 {
			g.clientMessage(u, "%s", line)
			line = ""
		}
		line += g.charName(o.id) + " "
	}
	if line != "" {
		g.clientMessage(u, "%s", line)
	}
	if count == 0 {
		g.clientMessage(u, "%s", g.lang(nnNoGuildMembers))
	}
	atWar := -1
	if war := g.guildWar[guild]; war > 0 && war < maxGuild {
		if g.guildWar[war] == guild {
			g.clientMessage(u, g.lang(snYouAreAtWar), g.guildName(war))
			atWar = war
		} else {
			g.clientMessage(u, g.lang(snWarToS), g.guildName(war))
		}
	}
	for other, target := range g.guildWar {
		if target == guild && other != atWar {
			g.clientMessage(u, g.lang(snWarFromS), g.guildName(other))
		}
	}
	if ally := g.guildAlly[guild]; ally > 0 && ally < maxGuild {
		g.clientMessage(u, g.lang(snAllyToS), g.guildName(ally))
	}
	for other, target := range g.guildAlly {
		if target == guild {
			g.clientMessage(u, g.lang(snAllyFromS), g.guildName(other))
		}
	}
}

// guildKingdomCape is the SendCharToWorld fix-up: a guild member whose
// cape is of another kingdom (or neutral) gets the guild's kingdom cape of
// the same tier.
func (g *Game) guildKingdomCape(pl *wire.Mob) {
	guild := int(pl.GuildIndex)
	if guild == 0 {
		return
	}
	kingdom := g.guild(guild).Kingdom
	if int(pl.CapeInfo) == kingdom {
		return
	}
	cape := pl.Equip[15].Index
	from := int(pl.CapeInfo) - 7
	white := false
	for _, id := range capesByKingdom[2] {
		white = white || id == cape
	}
	if white {
		from = 2
	}
	to := kingdom - 7
	if (!white && cape == 4006) || from < 0 || from > 2 || to < 0 || to > 2 {
		return
	}
	for i, id := range capesByKingdom[from] {
		if id == cape {
			pl.Equip[15].Index = capesByKingdom[to][i]
			return
		}
	}
}

// refreshMob is MulticastGetCreateMob: viewers see the new guild/medal.
func (g *Game) refreshMob(id int) {
	m := &g.mobs[id]
	p := g.createMobPacket(id)
	g.multicast2(int(m.target.X), int(m.target.Y), &p, 0)
}

func isGuildStaff(memberType int8) bool {
	return memberType == 9 || (memberType >= 3 && memberType <= 9)
}

// ---------------------------------------------------------------- recruit

// RecruitRequest mirrors MSG_RECRUITREQUEST (52 bytes), 0x680 / 0x681.
type RecruitRequest struct {
	Header    wire.Header
	Nickname  [16]byte
	GuildName [16]byte
	GuildID   int32
	ClientID  int32
}

// requestRecruit is RequestRecruitMember (0x3D5). The C++ put the player
// in the guild (and charged the 4,000,000) right away, without asking, and
// then the player's own "accept" always failed with "you have a guild".
// Here it only sends the invitation; requestAcceptRecruit does the rest.
func (g *Game) requestRecruit(u *User, pkt protocol.Packet) {
	var p wire.Parm2
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	target := int(p.Parm1)
	if target <= 0 || target >= MaxPlayer || target == u.id {
		return
	}
	tu := g.users[target]
	me, them := &g.mobs[u.id].info.Player, &g.mobs[target].info.Player
	if time.Now().Weekday() == time.Sunday {
		g.clientMessage(u, "%s", g.lang(nnGuildCantRecruitDay))
		return
	}
	if !isGuildStaff(me.GuildMemberType) || me.GuildIndex == 0 {
		g.clientMessage(u, "%s", g.lang(nnGuildCantKick))
		return
	}
	if tu.status != userPlay {
		g.clientMessage(u, "%s", g.lang(nnNotConnected))
		return
	}
	for _, id := range capesByKingdom[2] {
		if them.Equip[15].Index == id {
			g.clientMessage(u, "%s", g.lang(nnGuildCantRecruitKgd))
			return
		}
	}
	if them.CapeInfo != 0 && them.CapeInfo != me.CapeInfo {
		g.clientMessage(u, "%s", g.lang(nnGuildCantRecruitKgd))
		return
	}
	if them.GuildIndex != 0 {
		g.clientMessage(u, "Usuário possui uma guild")
		return
	}
	if me.Gold < recruitCost {
		g.clientMessage(u, g.lang(nnGuildCantRecruitGld), recruitCost)
		return
	}
	tu.guildInvite = guildInvite{from: u.id, guild: int(me.GuildIndex), at: time.Now()}
	req := RecruitRequest{Header: wire.NewHeader(opRecruitRequest, 0), GuildID: int32(me.GuildIndex), ClientID: int32(u.id)}
	copy(req.Nickname[:15], me.Name[:])
	wire.PutGameString(req.GuildName[:], g.guildName(int(me.GuildIndex)))
	g.queue(tu, &req)
	g.clientMessage(u, "Convite enviado para %s", g.charName(target))
}

// requestAcceptRecruit is RequestAcceptRecruit (0x681): the invited player
// says yes; the recruiter pays 4,000,000.
func (g *Game) requestAcceptRecruit(u *User, pkt protocol.Packet) {
	var p RecruitRequest
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	inv := u.guildInvite
	u.guildInvite = guildInvite{}
	from := int(p.ClientID)
	if inv.from == 0 || inv.from != from || int(p.GuildID) != inv.guild || time.Since(inv.at) > recruitTimeout {
		return
	}
	ru := g.users[from]
	me, rec := &g.mobs[u.id].info.Player, &g.mobs[from].info.Player
	if ru.status != userPlay || int(rec.GuildIndex) != inv.guild {
		return
	}
	if me.GuildIndex != 0 {
		g.clientMessage(u, "Você possui uma guild")
		return
	}
	if !isGuildStaff(rec.GuildMemberType) {
		g.clientMessage(u, "O outro jogador não é líder/sub da guild")
		return
	}
	if rec.Gold < recruitCost {
		g.clientMessage(u, "O outro jogador não possui o valor para recrutamento")
		return
	}
	me.GuildIndex, me.GuildMemberType = uint16(inv.guild), 1
	g.payGold(ru, recruitCost)
	g.clientMessage(u, "%s", g.lang(nnGuildRecruitEnter))
	g.clientMessage(ru, g.lang(nnGuildRecruit), g.charName(u.id))
	g.refreshMob(u.id)
	g.log.Info("guild recruit", "guild", inv.guild, "member", g.charName(u.id), "by", g.charName(from))
}

// requestKickMember is RequestKickMember (0x28C). Fixed: the target must be
// online, and nobody can kick the leader (the C++ let a sub-leader).
func (g *Game) requestKickMember(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	target := int(p.Value)
	if target <= 0 || target >= MaxPlayer || g.users[target].status != userPlay {
		return
	}
	me, them := &g.mobs[u.id].info.Player, &g.mobs[target].info.Player
	guild := int(me.GuildIndex)
	cant := func() { g.clientMessage(u, "%s", g.lang(nnGuildCantKick)) }
	if !isGuildStaff(me.GuildMemberType) || guild == 0 || int(them.GuildIndex) != guild {
		cant()
		return
	}
	t := them.GuildMemberType
	if t == 9 || (t >= 3 && t <= 5 && me.GuildMemberType != 9) {
		cant()
		return
	}
	if t >= 3 && t <= 5 {
		g.guildEntry(guild).Sub[t-3] = ""
		g.sendDB(&wire.AddSub{Header: wire.NewHeader(wire.OpAddSub, 0), GuildIndex: int16(guild), SubIndex: int16(t - 3), Status: 1})
	}
	g.leaveGuild(target)
	g.clientMessage(g.users[target], "%s", g.lang(nnGuildKicked))
	g.clientMessage(u, "%s", g.lang(nnGuildKicked))
	g.log.Info("guild kick", "guild", guild, "member", g.charName(target), "by", g.charName(u.id))
}

// leaveGuild clears the membership and stamps LastGuildKickOut.
func (g *Game) leaveGuild(id int) {
	m := &g.mobs[id]
	m.info.Player.GuildIndex, m.info.Player.GuildMemberType = 0, 0
	now := time.Now()
	m.info.LastGuildKickOut = wire.ItemDate{Ano: uint16(now.Year()), Mes: uint8(now.Month()), Dia: uint8(now.Day()),
		Hora: uint8(now.Hour()), Minuto: uint8(now.Minute()), Segundo: uint8(now.Second())}
	g.refreshMob(id)
}

// requestAlly is RequestAlly (0xE12 from the client): the leader asks
// DBSRV, which saves and rebroadcasts it to every channel (doAlly).
func (g *Game) requestAlly(u *User, pkt protocol.Packet) {
	var p wire.GuildRelation
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	a, b := int(p.GuildIndex1), int(p.GuildIndex2)
	if a <= 0 || b < 0 || a >= maxGuild || b >= maxGuild {
		return
	}
	pl := &g.mobs[u.id].info.Player
	// The C++ had the "own guild" check commented out, so a leader could
	// set the alliance of any guild; only his own is accepted here.
	if pl.GuildMemberType != 9 || int(pl.GuildIndex) != a {
		g.clientMessage(u, "%s", g.lang(nnCantDeclareAlly))
		return
	}
	cur := g.guildAlly[a]
	if (cur > 0 && cur < maxGuild && g.guildAlly[cur] == a) || g.guild(a).Kingdom != g.guild(b).Kingdom {
		g.clientMessage(u, "%s", g.lang(nnCantDeclareAlly))
		return
	}
	p.Header = wire.NewHeader(wire.OpGuildAlly, uint16(u.id))
	g.sendDB(&p)
}

// ---------------------------------------------------------------- commands

// guildCommand handles the guild slash commands; it reports whether cmd
// was one of them.
func (g *Game) guildCommand(u *User, cmd, value string) bool {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	guild := int(pl.GuildIndex)
	switch cmd {
	case "guild":
		if guild <= 0 {
			g.clientMessage(u, "Você não está em nenhuma guilda.")
		} else if pl.GuildMemberType == 9 {
			g.clientMessage(u, "Guild ID: %d || Fame Guild Atual: %d", guild, g.guild(guild).Fame)
		}

	case "guildfame":
		if guild > 0 && pl.GuildMemberType == 9 {
			g.clientMessage(u, "Você possui um total de %d pontos de fame guild", g.guild(guild).Fame)
		}

	case "criar", "create":
		g.createGuild(u, value)

	case "subcreate":
		g.createSubLeader(u, value)

	case "expulsar": // leave the guild (or drop the sub-leader medal)
		if value != "" {
			g.clientMessage(u, "Para expulsar um jogador, utilize o menu!")
			return true
		}
		if guild <= 0 || guild >= maxGuild {
			g.clientMessage(u, "%s", g.lang(nnNeedGuildMedal))
			return true
		}
		if t := pl.GuildMemberType; t >= 3 && t <= 5 {
			g.guildEntry(guild).Sub[t-3] = ""
			g.sendDB(&wire.AddSub{Header: wire.NewHeader(wire.OpAddSub, 0), GuildIndex: int16(guild), SubIndex: int16(t - 3), Status: 1})
			pl.GuildMemberType = 1
			g.clientMessage(u, "Medalha de sublíder destruída. Agora você é um membro comum")
			g.refreshMob(u.id)
		} else {
			g.leaveGuild(u.id)
			g.clientMessage(u, "%s", g.lang(nnGuildKicked))
		}

	case "handover": // pass the leadership on (the guild fame is lost)
		if guild == 0 {
			g.clientMessage(u, "Necessário possuir uma guild.")
			return true
		}
		other := g.userByName(value)
		if other == 0 {
			g.clientMessage(u, "O usuário não está conectado.")
			return true
		}
		op := &g.mobs[other].info.Player
		switch {
		case int(op.GuildIndex) != guild:
			g.clientMessage(u, "Necessário ser da mesma guilda.")
		case time.Now().Weekday() == time.Sunday:
			g.clientMessage(u, "Não é possível transferir no domingo")
		case pl.GuildMemberType != 9:
			g.clientMessage(u, "Transferência habilitada apenas para líderes de guild.")
		default:
			g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: 0, GuildIndex: int32(guild)})
			pl.GuildMemberType, op.GuildMemberType = op.GuildMemberType, pl.GuildMemberType
			g.clientMessage(u, "Medalha transferida")
			g.clientMessage(g.users[other], "Medalha transferida")
			g.refreshMob(u.id)
			g.refreshMob(other)
			g.log.Info("guild handover", "guild", guild, "from", g.charName(u.id), "to", g.charName(other))
		}

	case "fimirma": // end the alliance
		ally := g.guildAlly[guild]
		switch {
		case guild <= 0 || guild >= maxGuild || pl.GuildMemberType != 9:
			g.clientMessage(u, "%s", g.lang(nnOnlyGuildMasterCan))
		case ally <= 0 || ally >= maxGuild:
			g.clientMessage(u, "Você não possui aliança")
		case g.isWarTime():
			g.clientMessage(u, "Não é possível realizar em horário de guerra")
		default:
			g.sendDB(&wire.GuildRelation{Header: wire.NewHeader(wire.OpGuildAlly, 0), GuildIndex1: uint32(guild)})
		}

	default:
		return false
	}
	return true
}

// createGuild is "/criar nome": 100,000,000 gold or a guild ticket (4614).
// The guild id is the first free one of this server's range (guild numbers
// do not repeat between servers); DBSRV saves it and tells the other
// channels.
func (g *Game) createGuild(u *User, name string) {
	pl := &g.mobs[u.id].info.Player
	name = strings.TrimSpace(name)
	if name == "" || len(wire.EncodeGame(name)) > 15 {
		g.clientMessage(u, "%s", g.lang(nnGuildGuildName))
		return
	}
	id := -1
	first, last := cluster.GuildRange(g.cfg.Cluster.Group)
	for i := first; i <= last; i++ {
		if g.guild(i).Name == "" {
			id = i
			break
		}
	}
	if id <= 0 {
		g.clientMessage(u, "%s", g.lang(nnGuildLimit))
		return
	}
	ticket := firstSlotOf(pl, guildTicket)
	if ticket == -1 && pl.Gold < createGuildCost {
		g.clientMessage(u, "%s", g.lang(nnGuildNeed100mGold))
		return
	}
	if pl.GuildIndex != 0 {
		g.clientMessage(u, "%s", g.lang(nnGuildYouHaveAGuild))
		return
	}
	for _, x := range g.guilds {
		if strings.EqualFold(x.Name, name) {
			g.clientMessage(u, "%s", g.lang(nnGuildGuildName))
			return
		}
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return
	}
	p := wire.CreateGuild{Header: wire.NewHeader(wire.OpCreateGuild, uint16(u.id)),
		Kingdom: int32(pl.CapeInfo), Citizen: int32(g.cfg.Channel), GuildID: int32(id)}
	wire.PutGameString(p.GuildName[:], name)
	g.sendDB(&p)
	pl.GuildIndex, pl.GuildMemberType = uint16(id), 9
	if ticket == -1 {
		g.payGold(u, createGuildCost)
	} else {
		amountMinus(&pl.Inventory[ticket])
		g.sendItem(u, wire.SlotInv, ticket, &pl.Inventory[ticket])
	}
	g.guilds[id] = &Guild{Name: name, Kingdom: int(pl.CapeInfo), Citizen: g.cfg.Channel}
	g.refreshMob(u.id)
	g.clientMessage(u, "%s", g.lang(nnGuildSuccessCreate))
	g.log.Info("guild created", "guild", id, "name", name, "leader", g.charName(u.id), "kingdom", pl.CapeInfo)
}

// createSubLeader is "/subcreate nick subname" for 50,000,000 gold.
func (g *Game) createSubLeader(u *User, value string) {
	f := strings.Fields(value)
	if len(f) != 2 {
		g.clientMessage(u, "Comando inválido")
		return
	}
	pl := &g.mobs[u.id].info.Player
	guild := int(pl.GuildIndex)
	if guild == 0 || pl.GuildMemberType != 9 {
		g.clientMessage(u, "%s", g.lang(nnOnlyWithGuildMaster))
		return
	}
	other := g.userByName(f[0])
	if other == 0 {
		g.clientMessage(u, "%s", g.lang(nnNotConnected))
		return
	}
	op := &g.mobs[other].info.Player
	if int(op.GuildIndex) != guild {
		g.clientMessage(u, "%s", g.lang(nnOnlyGuildMemberCan))
		return
	}
	if pl.Gold < subLeaderCost {
		g.clientMessage(u, "%s", g.lang(nnNotEnoughGoldTele))
		return
	}
	x := g.guildEntry(guild)
	slot := -1
	for i, s := range x.Sub {
		if s == "" {
			slot = i
			break
		}
	}
	if slot == -1 {
		g.clientMessage(u, "%s", g.lang(nnGuildMaxSub))
		return
	}
	sub := f[1]
	if len(wire.EncodeGame(sub)) > 15 {
		sub = string([]rune(sub)[:15])
	}
	op.GuildMemberType = int8(3 + slot)
	x.Sub[slot] = sub
	g.refreshMob(other) // the C++ sent it to Target.X, Target.X
	p := wire.AddSub{Header: wire.NewHeader(wire.OpAddSub, uint16(u.id)), GuildIndex: int16(guild), SubIndex: int16(slot)}
	wire.PutGameString(p.Name[:], sub)
	g.sendDB(&p)
	g.clientMessage(u, "%s", g.lang(nnGuildSubCreated))
	g.clientMessage(g.users[other], "%s", g.lang(nnGuildSubCalled))
	g.payGold(u, subLeaderCost)
}

// userByName is GetUserByName: a playing character by name.
func (g *Game) userByName(name string) int {
	if name == "" {
		return 0
	}
	for _, u := range g.users[1:] {
		if u.status == userPlay && strings.EqualFold(g.charName(u.id), name) {
			return u.id
		}
	}
	return 0
}
