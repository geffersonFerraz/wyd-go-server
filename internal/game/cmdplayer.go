package game

import (
	"time"

	"dbsrv/internal/wire"
)

// The remaining player commands of RequestCommand.cpp: /daily, /roleta,
// /summonguild, /spk (/gritar), /passe, /attpass, /attrank, /novato,
// /honra, /quest, /evoc, the s<city> teleports, /tab, /nig, /snd, /relo,
// /summon and /registerbattle (the group finder ones are in
// groupfinder.go, /gm and /sgm in admin.go).
//
// Fixed:
//   - "/@9638" set the caller's level to 1010 - a backdoor to the admin
//     commands (they only checked level >= 1000); it is not ported;
//   - "/gm" spawned three bosses (generators 4818..4820) for anyone who
//     typed it, before checking anything;
//   - "/spk" sent an empty 0xD1D to the DB, so the other channels showed a
//     blank line; the message now goes to every channel;
//   - "/relo" accepted any pending "/summon" of the target, not one for
//     the caller.

const (
	nnCantSummonDead       = 48
	nnSummonNotAllowedHere = 49
	nnTooHighLevelToSummon = 50
	snSummonedBy           = 52
	nnPartyLeaderOnly      = 229
	nnMessageSND           = 354
	colorGreen             = 0xFF1DA907 // TNColor::Green
	trumpetItem            = 3330
)

// sCityCommands are the quiet town teleports (no random offset).
var sCityCommands = map[string][2]int{
	"sagua": {1965, 1770}, "sarmia": {2100, 2100}, "sgelo": {3653, 3130}, "serion": {2460, 2010},
	"sazran": {2479, 1691}, "skefra": {3248, 1696}, "spilar": {1178, 1722},
}

// chatTo is SendClientChat: a colored line for one player.
func (g *Game) chatTo(u *User, color uint32, msg string) {
	p := wire.ChatColor{Header: wire.NewHeader(opSrvChatColor, uint16(u.id)), Color: color}
	wire.PutGameString(p.Message[:96], msg)
	g.queue(u, &p)
}

// playerCommand runs the commands above; false when cmd is not one.
func (g *Game) playerCommand(u *User, cmd, value string, p *wire.Command) bool {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if xy, ok := sCityCommands[cmd]; ok {
		g.teleportar(u.id, xy[0], xy[1])
		if cmd == "sagua" || cmd == "sarmia" {
			g.signalParm(u, wire.ClientIDSystem, 0x3A1, 0) // CheckQuestTime = 0
		}
		return true
	}
	switch cmd {
	case "srv507":
	case "daily":
		g.sendDailyRewardInfo(u)
	case "roleta":
		g.roletaCommand(u)
	case "summonguild":
		g.summonGuildCommand(u)
	case "spk", "gritar":
		g.trumpet(u, value)
	case "passe":
		b := m.info.DailyQuest.BattlePass
		g.clientMessage(u, "Passe de Missões : %02d/%02d/%02d às %02d:%02d:%02d", b.Dia, b.Mes, b.Ano, b.Hora, b.Minuto, b.Segundo)
	case "gm", "sgm":
		g.adminCommand(u, value)
	case "@9638":
		g.log.Warn("backdoor command refused", "slot", u.id, "char", g.charName(u.id))
	case "attpass":
		g.attPass(u)
	case "attrank":
		g.getCurrentScore(u.id)
		g.checkGetLevel(u)
		g.sendScore(u.id)
		g.sendEtc(u)
	case "novato":
		g.novato(u)
	case "honra":
		g.clientMessage(u, "Honra Atual: %d", u.account.Honra)
	case "quest":
		g.questCommand(u)
	case "evoc":
		if pl.ClassInfo == 2 {
			g.signalParm(u, uint16(u.id), opSrvSummonTime, 1)
			g.dismissSummons(u.id)
		}
	case "tab":
		if pl.Status.Level < 69 && evolution(pl) == evoMortal {
			g.clientMessage(u, "Somente acima do level 70")
			return true
		}
		m.tab = [26]byte{}
		wire.PutCString(m.tab[:25], wire.CString(p.Value[:]))
		g.refreshMob(u.id)
	case "nig":
		now := time.Now()
		g.clientMessage(u, "!!%02d%02d%02d", now.Hour(), now.Minute(), now.Second())
	case "snd":
		if value == "" {
			u.snd = ""
			return true
		}
		if r := []rune(value); len(r) > 95 {
			value = string(r[:95])
		}
		u.snd = value
		g.clientMessage(u, g.lang(nnMessageSND), value)
	case "relo":
		g.reloCommand(u, value)
	case "summon":
		g.summonCommand(u, value)
	case "evoc2":
		// The client sends it with every party invite; the C++ did not know
		// it and answered "not connected" (a whisper to nobody).
		g.sendPartyEvocation(u.id)
	case "registerbattle":
		g.clientMessage(u, "Evento associado com este NPC não encontrado") // the arena event is not ported
	default:
		return false
	}
	return true
}

// summonGuildCommand is "/summonguild": the guild master calls every
// member online to where they stand.
func (g *Game) summonGuildCommand(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	x, y := int(m.target.X), int(m.target.Y)
	if pl.GuildMemberType != 9 {
		g.clientMessage(u, "%s", g.lang(nnOnlyGuildMasterCan2))
		return
	}
	if a := g.data.attribute(x, y); a&attrCantSummon != 0 || a&attrPvP != 0 || g.data.village(x, y) == 5 {
		g.clientMessage(u, "%s", g.lang(nnCantUseThatHere))
		return
	}
	g.summonGuild(int(pl.GuildIndex), x, y, MaxPlayer)
}

// summonGuild is SummonGuild(guild, x, y, max).
func (g *Game) summonGuild(guild, x, y, limit int) {
	if guild <= 0 {
		return
	}
	n := 0
	for i := 1; i < MaxPlayer; i++ {
		if g.users[i].status != userPlay || g.mobs[i].mode == 0 || int(g.mobs[i].info.Player.GuildIndex) != guild {
			continue
		}
		g.teleportar(i, x, y)
		n++
		if n == 30 {
			x, y = x+1, y+1
		}
		if n == 45 {
			x, y = x-2, y-2
		}
		if n >= limit {
			break
		}
	}
}

// trumpet is "/spk text": a Trombeta (3330) shouts to every channel.
func (g *Game) trumpet(u *User, value string) {
	pl := &g.mobs[u.id].info.Player
	slot := firstSlotOf(pl, trumpetItem)
	if slot == -1 {
		return
	}
	amountMinus(&pl.Inventory[slot])
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	if r := []rune(value); len(r) > 80 {
		value = string(r[:80])
	}
	msg := "[" + g.charName(u.id) + "] : " + value
	if g.dbUp {
		p := wire.ChatColor{Header: wire.NewHeader(opSrvChatColor, uint16(u.id)), Color: colorGreen}
		wire.PutGameString(p.Message[:96], msg)
		g.sendDB(&p) // DBSRV relays it to every channel, this one included
	} else {
		g.chatAll(colorGreen, msg)
	}
	g.log.Info("trumpet", "slot", u.id, "text", value)
}

// relayChat is the 0xD1D from the DB: a trumpet for everyone here.
func (g *Game) relayChat(raw []byte) {
	b := append([]byte(nil), raw...)
	if len(b) >= 8 {
		le16(b[6:], wire.ClientIDSystem)
	}
	for _, u := range g.users[1:] {
		if u.status == userPlay {
			g.queueRaw(u, b)
		}
	}
}

// novato is "/novato": once per account, 30 minutes of the frango (30)
// and exp (39) buffs.
func (g *Game) novato(u *User) {
	if u.account.Novato == 1 {
		return
	}
	for _, idx := range []int{30, 39} {
		slot := g.emptyAffect(u.id, idx)
		if slot < 0 {
			continue
		}
		a := &g.mobs[u.id].info.Affects[slot]
		if int(a.Index) == idx {
			a.Time += 3600 / 2
		} else {
			*a = wire.Affect{Index: uint8(idx), Value: 1, Time: 3600 / 2}
		}
	}
	g.log.Info("novato used", "slot", u.id)
	u.account.Novato = 1
	g.saveUser(u, 1)
	g.sendScore(u.id)
	g.sendEtc(u)
}

// questCommand is "/quest": the newbie quest area for the level.
func (g *Game) questCommand(u *User) {
	level := g.mobs[u.id].info.Player.Status.Level
	switch {
	case level >= 320:
		g.clientMessage(u, "Bem Vindo a Quest Elfos!")
		g.teleportar(u.id, 1300, 4036)
	case level >= 265:
		g.clientMessage(u, "Bem Vindo a Quest Hidra!")
		g.teleportar(u.id, 668, 3769)
	case level >= 190:
		g.clientMessage(u, "Bem Vindo a Quest Kaizen!")
		g.teleportar(u.id, 454, 3910)
	case level >= 115:
		g.clientMessage(u, "Bem Vindo a Quest Jardim de Deus!")
		g.teleportar(u.id, 2224, 1712)
	default:
		g.clientMessage(u, "Bem Vindo a Quest do Coveiro!")
		g.teleportar(u.id, 2371, 2101)
	}
}

// summonCommand is "/summon nick" (ClassInfo 1 with the skill bit 0x40000):
// ask nick to come; "/relo" answers it.
func (g *Game) summonCommand(u *User, nick string) {
	target := g.userByName(nick)
	if target <= 0 {
		g.clientMessage(u, "%s", g.lang(nnNotConnected))
		return
	}
	pl := &g.mobs[u.id].info.Player
	if pl.ClassInfo != 1 || pl.Learn[0]&0x40000 == 0 {
		g.clientMessage(u, "Só pode usar quando convocado pelo grupo ou pelo Líder de Guild")
		return
	}
	g.sendSummonCall(u, target)
	g.log.Info("summon request", "slot", u.id, "target", nick)
}

func (g *Game) sendSummonCall(u *User, target int) {
	p := SummonCall{Header: wire.NewHeader(opSummonCall, wire.ClientIDSystem)}
	name := wire.CString(g.mobs[u.id].info.Player.Name[:])
	if len(name) >= 12 {
		name = name[:11]
	}
	copy(p.Nickname[:], name)
	g.queue(g.users[target], &p)
	u.summonedUser = target
}

// summonSkill is the instance type 9 branch of RequestAttack: the summon
// skill asks a player to come.
func (g *Game) summonSkill(u *User, tid, mastery int) {
	me, t := &g.mobs[u.id], &g.mobs[tid]
	if t.info.Player.Status.CurHP <= 0 {
		g.clientMessage(u, "%s", g.lang(nnCantSummonDead))
		return
	}
	if g.data.attribute(int(me.target.X), int(me.target.Y))&attrCantSummon != 0 && me.info.Player.Status.Level < 1000 {
		g.clientMessage(u, "%s", g.lang(nnSummonNotAllowedHere))
		return
	}
	if int(t.info.Player.BStatus.Level) > int(me.info.Player.BStatus.Level)+mastery+30 {
		g.clientMessage(u, "%s", g.lang(nnTooHighLevelToSummon))
		return
	}
	if t.target.X&0xFF00 == 0 && t.target.Y&0xFF00 == 0 {
		g.clientMessage(u, "%s", g.lang(nnCantUseThatHere))
		return
	}
	if tid >= MaxPlayer || g.users[tid].status != userPlay {
		return
	}
	g.sendSummonCall(u, tid)
	g.clientMessage(g.users[tid], g.lang(snSummonedBy), g.charName(u.id))
}

// reloCommand is "/relo nick": go to a party or guild member with a portal
// scroll (699, 776 or 3430). Players other than ClassInfo 1 need nick to
// have summoned them.
func (g *Game) reloCommand(u *User, nick string) {
	to := g.userByName(nick)
	if to <= 0 {
		g.clientMessage(u, "%s", g.lang(nnNotConnected))
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	tu := g.users[to]
	accepted := false
	if pl.ClassInfo != 1 {
		if tu.summonedUser != u.id {
			g.clientMessage(u, "Só pode usar quando convocado pelo grupo ou pelo Líder de Guild")
			return
		}
		accepted = true
	}
	defer func() {
		if accepted {
			tu.summonedUser = 0
		}
	}()
	if pl.Status.CurHP <= 0 {
		g.clientMessage(u, "Você não pode ir até o usuario, pois esta morto.")
		return
	}
	leader := m.npc.leader
	if leader == 0 {
		leader = u.id
	}
	canMove := leader == to || g.sameParty(leader, to)
	if gi := pl.GuildIndex; gi != 0 && gi == g.mobs[to].info.Player.GuildIndex {
		canMove = true
	}
	tm := &g.mobs[to]
	if g.data.attribute(int(tm.target.X), int(tm.target.Y))&attrCantSummon != 0 {
		canMove = false
	}
	slot := -1
	for _, id := range []uint16{699, 776, 3430} {
		if slot = firstSlotOf(pl, id); slot != -1 {
			break
		}
	}
	if slot == -1 {
		g.clientMessage(u, "Necessario Pergaminho Portal para dar relo")
		return
	}
	if !canMove {
		g.clientMessage(u, "%s", g.lang(nnCantUseThatHere))
		return
	}
	amountMinus(&pl.Inventory[slot])
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	g.teleportar(u.id, int(tm.target.X), int(tm.target.Y))
	g.log.Info("relo", "slot", u.id, "to", nick, "x", tm.target.X, "y", tm.target.Y)
}
