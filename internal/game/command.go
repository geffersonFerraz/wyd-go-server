package game

import (
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Slash commands and the chats that ride on 0x334 (RequestCommand.cpp):
// "@" kingdom / "@@" citizen chat, "=" party chat, "-" guild / "--" guild
// and ally chat, "/r" and "/nick text" whispers, guild commands
// (guild.go), store commands (auction.go), the group finder
// (groupfinder.go), the other player commands (cmdplayer.go) and the
// admin ones (admin.go).

// cityCommands are the town teleports (x, y; a random 0..2 is added).
var cityCommands = map[string]struct {
	x, y int
	msg  string
}{
	"armia": {2100, 2100, "Teleportado para cidade de Armia."},
	"arzan": {2487, 1712, "Teleportado para cidade de Arzan."},
	"erion": {2462, 2008, "Teleportado para cidade de Erion."},
	"gelo":  {3650, 3127, "Teleportado para cidade de Niffleheim ."},
	"kefra": {3250, 1695, "Teleportado para cidade do Kefra."},
	"pilar": {1179, 1721, "Area Pilar."},
	"agua":  {1965, 1769, "Area Pergaminho Agua."},
}

func (g *Game) requestCommand(u *User, pkt protocol.Packet) {
	var p wire.Command
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	p.Command[15] = 0
	p.Value[99] = 0
	cmd := wire.CString(p.Command[:])
	raw := wire.CString(p.Value[:]) // Windows-1252 bytes
	value := wire.DecodeGame(raw)
	m := &g.mobs[u.id]
	pl := &m.info.Player

	if c, ok := cityCommands[cmd]; ok {
		if cmd == "kefra" {
			g.sendEtc(u)
		}
		g.teleportar(u.id, c.x+rnd()%3, c.y+rnd()%3)
		g.clientMessage(u, "%s", c.msg)
		return
	}
	if g.guildCommand(u, cmd, value) || g.storeCommand(u, cmd) || g.playerCommand(u, cmd, value, &p) {
		return
	}
	switch {
	case cmd == "day":
		g.clientMessage(u, "!#11 2")
		return
	case cmd == "cp":
		g.clientMessage(u, g.lang(nnCPPoint), pkPoint(pl)-75)
		return
	case cmd == "nt":
		if v := m.info.PesaEnter; v <= 0 {
			g.clientMessage(u, "%s", g.lang(nnNTZero))
		} else {
			g.clientMessage(u, g.lang(nnNTAmount), v)
		}
		return
	case cmd == "time":
		g.clientMessage(u, "%s", time.Now().Format("15:04:05 02-01-2006"))
		return
	case cmd == "kingdom" || cmd == "Kingdom" || cmd == "reino" || cmd == "Reino":
		g.kingdomCommand(u)
		return
	case cmd != "" && g.groupCommand(u, cmd, &p):
		return
	}

	// Chats: the packet goes on with the sender's name as the command.
	if cmd == "" && raw != "" {
		wire.PutCString(p.Command[:13], wire.CString(pl.Name[:]))
		switch raw[0] {
		case '@':
			g.kingdomChat(u, &p, len(raw) > 1 && raw[1] == '@')
		case '=':
			g.partyChat(u, &p)
		case '-':
			g.guildChat(u, &p, len(raw) > 1 && raw[1] == '-')
		default:
			g.clientMessage(u, "%s", g.lang(nnNotConnected)) // a whisper to nobody
		}
		return
	}

	// Whisper: "/r text" answers the last one, "/nick text" talks to nick.
	target := 0
	if cmd == "r" {
		target = u.lastWhisper
		if target <= 0 || target >= MaxPlayer || g.users[target].status != userPlay {
			target = 0
		}
	} else {
		target = g.userByName(wire.DecodeGame(cmd))
	}
	g.whisper(u, target, raw)
}

func (g *Game) kingdomChat(u *User, p *wire.Command, citizen bool) {
	if time.Since(u.citizenChatAt) <= 5*time.Second && u.accessLevel == 0 {
		g.clientMessage(u, "Tempo mínimo para enviar outra mensagem é de 5 segundos.")
		return
	}
	m := &g.mobs[u.id].info
	kingdom := m.Player.CapeInfo
	for _, o := range g.users[1:] {
		if o.status != userPlay || o.id == u.id {
			continue
		}
		om := &g.mobs[o.id].info
		if !citizen {
			if o.kingdomChatOff {
				continue
			}
			if om.Player.CapeInfo == 0 || om.Player.CapeInfo == kingdom || kingdom == 0 || o.accessLevel != 0 {
				g.queue(o, p)
			}
		} else if !o.citizenChatOff && (om.Citizen == m.Citizen || o.accessLevel != 0) {
			g.queue(o, p)
		}
	}
	u.citizenChatAt = time.Now()
}

func (g *Game) partyChat(u *User, p *wire.Command) {
	leader := g.mobs[u.id].npc.leader
	if leader <= 0 {
		leader = u.id
	}
	for _, id := range g.mobs[leader].npc.followers {
		if id <= 0 || id >= MaxPlayer || id == u.id || g.users[id].status != userPlay {
			continue
		}
		g.queue(g.users[id], p)
	}
	if leader != u.id && g.users[leader].status == userPlay {
		g.queue(g.users[leader], p)
	}
}

func (g *Game) guildChat(u *User, p *wire.Command, withAlly bool) {
	guild := int(g.mobs[u.id].info.Player.GuildIndex)
	if guild == 0 {
		g.clientMessage(u, "%s", g.lang(nnOnlyGuildMemberCan))
		return
	}
	ally := g.guildAlly[guild]
	if ally == 0 {
		ally = -1
	}
	for _, o := range g.users[1:] {
		if o.status != userPlay || o.id == u.id || o.guildChatOff {
			continue
		}
		og := int(g.mobs[o.id].info.Player.GuildIndex)
		if og == guild || (withAlly && og == ally) {
			g.queue(o, p)
		}
	}
}

// whisper sends text to target, or with no text shows who the target is.
func (g *Game) whisper(u *User, target int, text string) {
	if target == 0 {
		g.clientMessage(u, "%s", g.lang(nnNotConnected))
		return
	}
	tu := g.users[target]
	if tu.whisperOff {
		g.clientMessage(u, "%s", g.lang(nnWhisperMP))
		return
	}
	ti := &g.mobs[target].info
	if text == "" {
		name := g.charName(target)
		if gi := int(ti.Player.GuildIndex); gi != 0 {
			g.clientMessage(u, "Nome: %s /  Fame: %d / Cidadania: %d / Guild: %s", name, ti.Fame, ti.Citizen, g.guildName(gi))
		} else {
			g.clientMessage(u, "Nome: %s /  Fame: %d / Cidadania: %d", name, ti.Fame, ti.Citizen)
		}
		g.showSND(u, tu)
		return
	}
	u.lastWhisper, tu.lastWhisper = target, u.id
	out := wire.Command{Header: wire.NewHeader(wire.OpCliCommand, uint16(target))}
	wire.PutCString(out.Command[:], wire.CString(g.mobs[u.id].info.Player.Name[:]))
	wire.PutCString(out.Value[:100], text)
	g.queue(tu, &out)
	g.showSND(u, tu)
}

// showSND shows the target's "/snd" away message to whoever whispers it.
func (g *Game) showSND(u, target *User) {
	if target.snd != "" {
		g.clientMessage(u, g.lang(nnMessageSND), target.snd)
	}
}

// kingdomCommand is "/kingdom": with a portal scroll (699, 776 or 3430),
// to the kingdom's castle area.
func (g *Game) kingdomCommand(u *User) {
	pl := &g.mobs[u.id].info.Player
	slot := -1
	for _, id := range []uint16{699, 776, 3430} {
		if slot = firstSlotOf(pl, id); slot != -1 {
			break
		}
	}
	if slot == -1 {
		g.clientMessage(u, "Necessário ter um pergaminho do portal no inventário.")
		return
	}
	amountMinus(&pl.Inventory[slot])
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	switch pl.CapeInfo {
	case 7:
		g.teleportar(u.id, 1690, 1617)
	case 8:
		g.teleportar(u.id, 1690, 1835)
	default:
		g.teleportar(u.id, 1701, 1726)
	}
}
