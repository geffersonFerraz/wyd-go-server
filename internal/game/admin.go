package game

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// adminCommand is HandleAdminCommand, reached only from the admin panel
// (0xC1A). u is the character it acts on, or g.users[0] for the commands of
// the whole channel. It returns what the command answered.
//
// Fixed:
//   - sscanf/strncmp quirks made several commands unreachable ("+exp n"
//     compared 8 characters of "+exp", "+generate n" 13, "+equip" 10,
//     "+runes" parsed "+set runes", "+guildhour" parsed "+guildhonour",
//     "+guildchall2" was shadowed by "+guildchall"); the arguments are
//     parsed by words here;
//   - "+dcall" always answered "0 usuarios".
//
// Commands for systems this server does not have (fake stores, renaming,
// reloads of the C++ text files) answer that they are not supported.
func (g *Game) adminCommand(u *User, value string) []string {
	g.adminOut = nil
	g.runAdminCommand(u, value)
	return g.adminOut
}

// adminSay is the answer of a command, shown in the panel.
func (g *Game) adminSay(format string, args ...any) {
	g.adminOut = append(g.adminOut, fmt.Sprintf(format, args...))
}

// adminChannelCommands act on the whole channel or take the name as an
// argument, so they need no target character.
var adminChannelCommands = map[string]bool{
	"+dcall": true, "+on": true, "+hpmob": true, "+generate": true, "+gate": true, "+guildowner": true, "+resetcities": true, "+fame": true,
	"+removeall": true, "+remove": true, "+kick": true, "+teleport": true, "+ban": true, "+banmac": true, "+not": true,
	"+reloadcash": true, "+weekmode": true, "+createkefra": true, "+kill": true, "+weather": true, "+bonusexp": true,
	"+drop": true, "+reloadban": true,
}

func (g *Game) runAdminCommand(u *User, value string) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if value == "" {
		g.adminSay("Informe o comando")
		return
	}
	f := strings.Fields(value)
	if len(f) == 0 {
		g.adminSay("Informe o comando")
		return
	}
	cmd := f[0]
	if u.id == 0 && !adminChannelCommands[cmd] {
		g.adminSay("Este comando precisa de um personagem online neste canal")
		return
	}
	args := f[1:]
	num := func(i int) (int, bool) {
		if i >= len(args) {
			return 0, false
		}
		v, err := strconv.Atoi(args[i])
		return v, err == nil
	}
	rest := func(i int) string {
		if i >= len(args) {
			return ""
		}
		return strings.Join(args[i:], " ")
	}
	itemArgs := func(from int) (wire.Item, bool) {
		id, ok := num(from)
		if !ok {
			return wire.Item{}, false
		}
		it := wire.Item{Index: uint16(id)}
		for k := 0; k < 3; k++ {
			ef, _ := num(from + 1 + 2*k)
			efv, _ := num(from + 2 + 2*k)
			it.Effect[k] = wire.ItemEffect{Index: uint8(ef), Value: uint16(efv)}
		}
		return it, true
	}
	g.log.Info("admin command", "slot", u.id, "char", g.charName(u.id), "command", value)

	switch cmd {
	case "+item":
		it, ok := itemArgs(0)
		if !ok {
			g.adminSay("Comando invalido")
			return
		}
		if it.Effect[0].Index == 99 {
			it = wire.Item{Index: it.Index}
			g.setItemBonusLevel(&it, int(pl.Status.Level), 0, 100)
		}
		slot := firstCarrySlot(&pl.Inventory, it.Index)
		if slot == -1 {
			g.adminSay("%s", g.lang(nnYouHaveNoSpaceToTrade))
			return
		}
		pl.Inventory[slot] = it
		g.sendItem(u, wire.SlotInv, slot, &it)

	case "+frag":
		x, _ := num(0)
		setTotKill(pl, x)
		g.refreshMob(u.id)
		g.adminSay("FRAG")

	case "+move":
		x, ok1 := num(0)
		y, ok2 := num(1)
		if !ok1 || !ok2 || !inMap(x, y) {
			g.adminSay("Coordenadas invalidas")
			return
		}
		g.teleportar(u.id, x, y)

	case "+hpmob":
		hp, ok := num(0)
		name := rest(1)
		if !ok || name == "" {
			g.adminSay("Comando invalido")
			return
		}
		for i := MaxPlayer; i < MaxMob; i++ {
			o := &g.mobs[i]
			if o.mode == 0 || o.info.Player.Status.CurHP <= 0 || g.charName(i) != name {
				continue
			}
			o.info.Player.Status.CurHP = int64(hp)
			g.refreshMob(i)
			g.adminSay("HPmob setado")
			return
		}
		g.adminSay("Mob não encontrado")

	case "+snoop":
		if pl.Info&1 != 0 {
			g.adminSay("SNOOP OFF")
			pl.Info &^= 1
		} else {
			g.adminSay("SNOOP ON")
			pl.Info |= 1
		}
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.refreshMob(u.id)

	case "+dcall":
		n := 0
		for _, o := range g.users[1:] {
			if o.status < userSelChar || o.status == userEmpty {
				continue
			}
			g.clientMessage(o, "Servidor entrando em manutenção...")
			g.flush(o)
			if o != u {
				g.closeUser(o)
				n++
			}
		}
		g.adminSay("Foram desconectados %d usuarios", n)
		if u.id != 0 {
			g.closeUser(u)
		}

	case "+on":
		total, machines := 0, 0
		for x := 1; x < MaxPlayer; x++ {
			if g.users[x].status < userSelChar {
				continue
			}
			total++
			dup := false
			for t := x + 1; t < MaxPlayer; t++ {
				if g.users[t].status >= userSelChar && g.users[t].mac == g.users[x].mac {
					dup = true
					break
				}
			}
			if !dup {
				machines++
			}
		}
		g.adminSay("Total de Onlines: %d com %d computadores", total, machines)

	case "+exp":
		v, err := strconv.ParseInt(rest(0), 10, 64)
		if err != nil || v < 0 {
			g.adminSay("Indice invalido")
			return
		}
		pl.Exp = v
		g.sendScore(u.id)
		g.sendEtc(u)

	case "+generate":
		gen, ok := num(0)
		if !ok || gen < 0 || gen >= len(g.data.Geners) {
			g.adminSay("Indice invalido")
			return
		}
		g.generateMob(gen, 0, 0)
		ge := &g.data.Geners[gen]
		g.adminSay("Criado o mob %s em %dx %dy", wire.DecodeGame(wire.CString(ge.Leader.Name[:])), ge.SegmentX[0], ge.SegmentY[0])

	case "+gate":
		x, ok := num(0)
		if !ok || x < 0 || x >= maxGroundItem {
			return
		}
		status, word := 3, "fechado"
		if len(args) > 1 && args[1] == "abrir" {
			status, word = 1, "aberto"
		}
		g.setDoors(x, 1, status, true)
		gi := &g.ground[x]
		g.adminSay("Portao %d %s - %dx %dy", x, word, gi.x, gi.y)

	case "+cash":
		x, ok := num(0)
		if !ok {
			return
		}
		u.account.Cash = uint32(x)
		g.adminSay("SET CASH")
		g.sendClientInfo(u)

	case "+set":
		if len(args) >= 2 && args[0] == "mission" {
			x, _ := strconv.Atoi(args[1])
			m.info.DailyQuest.QuestID = int32(x)
			m.info.DailyQuest.IsAccepted = false
			g.sendMissionInfo(u)
			g.adminSay("Setado missão")
			return
		}
		g.adminUnsupported(value)

	case "+level":
		x, ok := num(0)
		if !ok || x < 0 || x > 0x7FFF {
			g.adminSay("Level invalido")
			return
		}
		pl.BStatus.Level = int16(x)
		pl.Status.Level = pl.BStatus.Level
		pl.Exp = acctedit.LevelExp(evolution(pl), x) // the C++ gave the next level's exp
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendEtc(u)
		g.refreshMob(u.id)

	case "+equip":
		x, ok := num(0)
		it, ok2 := itemArgs(1)
		if !ok || !ok2 || x < 0 || x >= len(pl.Equip) {
			g.adminSay("Comando inválido")
			return
		}
		pl.Equip[x] = it
		g.sendEtc(u)
		g.sendEquip(u)
		g.getCurrentScore(u.id)
		g.sendItem(u, wire.SlotEquip, x, &pl.Equip[x])

	case "+limparinv":
		for i := range invSlots {
			pl.Inventory[i] = wire.Item{}
			g.sendItem(u, wire.SlotInv, i, &pl.Inventory[i])
		}
		g.adminSay("SET EMPTY CARRY")

	case "+limparbau":
		for i := range u.account.StorageItems {
			u.account.StorageItems[i] = wire.Item{}
			g.sendItem(u, wire.SlotStorage, i, &u.account.StorageItems[i])
		}
		g.adminSay("SET EMPTY BAU")

	case "+guildowner", "+resetcities":
		// City owners live in the database (gamedata.city_owners) and are
		// managed only in the admin panel; these commands changed only this
		// channel's memory and were undone by the next reload.
		g.adminSay("Donos de cidade agora são definidos só pelo painel admin (Guildas > Cidades).")

	case "+fame":
		x, ok1 := num(0)
		y, ok2 := num(1)
		if !ok1 || !ok2 || x <= 0 || x >= maxGuild {
			return
		}
		g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: int32(y), GuildIndex: int32(x)})
		g.adminSay("Fame Enviada com sucesso")

	case "+special":
		x, ok1 := num(0)
		y, ok2 := num(1)
		if !ok1 || !ok2 || x < 0 || x >= 4 || y < 0 || y > 255 {
			g.adminSay("Comando invalido")
			return
		}
		pl.BStatus.Mastery[x] = uint16(y)
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.adminSay("SET SPECIAL")

	case "+removeall", "+remove":
		prefix := rest(0)
		if prefix == "" {
			g.adminSay("Mob não encontrado")
			return
		}
		n := 0
		for i := MaxPlayer; i < MaxMob; i++ {
			if g.mobs[i].mode == 0 || !strings.HasPrefix(g.charName(i), prefix) {
				continue
			}
			x, y := g.mobs[i].target.X, g.mobs[i].target.Y
			g.deleteMob(i, 1)
			n++
			if cmd == "+remove" {
				g.adminSay("Mob %s removido de %dx %dy", prefix, x, y)
				return
			}
		}
		switch {
		case n == 0 && cmd == "+remove":
			g.adminSay("Mob não encontrado")
		case n == 0:
			g.adminSay("Nao foi encontado mobs")
		default:
			g.adminSay("Foram removidos %d mobs %s", n, prefix)
		}

	case "+buff":
		x, ok := num(0)
		if !ok {
			return
		}
		m.info.Affects[0] = wire.Affect{Index: uint8(x), Time: 162000}
		g.sendScore(u.id)
		g.sendAffect(u)
		g.getCurrentScore(u.id)
		g.adminSay("SET BUFF")

	case "+nobuff":
		m.info.Affects = [len(m.info.Affects)]wire.Affect{}
		g.sendScore(u.id)
		g.getCurrentScore(u.id)
		g.adminSay("BUFF OFF")
		g.sendAffect(u)

	case "+summonserver":
		n := 0
		for i := 1; i < MaxPlayer; i++ {
			if g.users[i].status != userPlay || i == u.id {
				continue
			}
			x, y := int(m.target.X), int(m.target.Y)
			if n > 50 {
				x, y = x+3, y+3
			} else {
				x, y = x+10, y+10
			}
			g.teleportar(i, x, y)
			n++
		}
		g.adminSay("Summonado %d personagens...", n)

	case "+summon", "+kick", "+teleport", "+relo", "+ban":
		if len(args) == 0 {
			g.adminSay("Comando invalido")
			return
		}
		to := g.userByName(args[0])
		if to <= 0 {
			if cmd == "+ban" {
				g.adminSay("Usuário não está conectado para banir")
			} else {
				g.adminSay("Usuario não esta conectado")
			}
			return
		}
		switch cmd {
		case "+summon":
			g.teleportar(to, int(m.target.X), int(m.target.Y))
			g.adminSay("Summonou %s para %dx %dy", g.charName(to), m.target.X, m.target.Y)
		case "+kick":
			g.closeUser(g.users[to])
			g.adminSay("%s - %s foi kickado - ClientId %d", g.charName(u.id), args[0], to)
		case "+teleport":
			x, ok1 := num(1)
			y, ok2 := num(2)
			if ok1 && ok2 && inMap(x, y) {
				g.teleportar(to, x, y)
			}
		case "+relo":
			g.teleportar(u.id, int(g.mobs[to].target.X), int(g.mobs[to].target.Y))
		case "+ban":
			typ, _ := num(1)
			dia, _ := num(2)
			mes, _ := num(3)
			ano, _ := num(4)
			tu := g.users[to]
			tu.account.BanType = uint32(typ)
			tu.account.Ban = wire.ItemDate{Dia: uint8(dia), Mes: uint8(mes), Ano: uint16(ano)}
			g.closeUser(tu)
			g.adminSay("Usuário banido e desconectado!")
		}

	case "+coin":
		x, ok := num(0)
		if !ok {
			return
		}
		pl.Gold = int64(x)
		g.sendEtc(u)
		g.adminSay("Gold setado para: %d.", x)

	case "+not":
		msg := rest(0)
		if msg == "" {
			g.adminSay("Digite uma mensagem")
			return
		}
		g.notice(msg)

	case "+reloadcash":
		g.adminSay("Loja donate atualizada!")

	case "+status":
		if len(args) == 0 {
			g.adminSay("Comando invalido")
			return
		}
		mode := -1
		for i, s := range []string{"str", "dex", "int", "con"} {
			if strings.HasPrefix(strings.ToLower(args[0]), s) {
				mode = i
			}
		}
		if mode == -1 {
			idx, ok := num(0)
			it, ok2 := itemArgs(1)
			if !ok || !ok2 {
				g.adminSay("Comando invalido")
				return
			}
			if idx < 0 || idx > 15 {
				g.adminSay("Indice invalido")
				return
			}
			pl.Equip[idx] = it
			g.sendItem(u, wire.SlotEquip, idx, &it)
			return
		}
		x, _ := num(1)
		switch mode {
		case 0:
			pl.BStatus.STR = int64(x)
		case 1:
			pl.BStatus.DEX = int64(x)
		case 2:
			pl.BStatus.INT = int64(x)
		case 3:
			pl.BStatus.CON = int64(x)
		}
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendEtc(u)

	case "+learn":
		x, ok := num(0)
		if !ok || x < 0 || x >= 64 {
			g.adminSay("Comando invalido")
			return
		}
		pl.Learn[x/32] ^= 1 << uint(x%32)
		g.sendEtc(u)
		g.adminSay("SET LEARN")

	case "+weekmode":
		x, ok := num(0)
		if !ok || x < 0 || x > 5 {
			g.adminSay("Comando invalido")
			return
		}
		g.war.forceWeek, g.war.weekMode = x, x-1
		if x == 0 {
			g.war.weekMode = weekBetting
		}
		g.adminSay("SET WEEKMODE")

	case "+createkefra":
		g.rebornKefra()

	case "+kill":
		x, ok := num(1)
		if len(args) < 2 || args[0] != "kefra" || !ok {
			g.adminSay("Comando invalido")
			return
		}
		g.kefra.killer, g.kefra.dead = int32(x), true
		for id := firstNPC; id < MaxMob; id++ {
			if g.mobs[id].mode != 0 && g.mobs[id].npc.generateID == genKefra {
				g.deleteMob(id, 1)
			}
		}

	case "+weather":
		x, ok := num(0)
		if !ok || x < 0 || x > 2 {
			g.adminSay("Comando invalido")
			return
		}
		g.weather = x
		g.sendWeather()
		g.adminSay("SET WEATHER")

	case "+bonusexp":
		x, ok := num(0)
		if !ok || x < 0 {
			g.adminSay("Comando invalido")
			return
		}
		g.expRate = x
		g.adminSay("Bônus exp setado para: %d.", x)

	case "+banmac":
		id := 0
		if len(args) > 0 {
			id = g.userByName(args[0])
		}
		if id <= 0 {
			g.adminSay("Jogador não encontrado")
			return
		}
		g.banMac(u, g.users[id])

	case "+drop":
		x, ok := num(0)
		if !ok || x < 0 {
			g.adminSay("Comando invalido")
			return
		}
		g.dropAll = x
		g.adminSay("Drop setado para: %d.", x)

	case "+reloadban":
		g.adminSay("Os banimentos por computador vêm do banco e são atualizados sozinhos.")

	case "+create":
		g.adminCreateMob(u, args)

	default:
		g.adminUnsupported(value)
	}
}

// adminCreateMob is +create: spawns NPCs by name next to the GM.
func (g *Game) adminCreateMob(u *User, args []string) {
	if len(args) == 0 {
		g.adminSay("Utilize +create nome quantidade")
		return
	}
	count := 1
	if len(args) > 1 {
		if n, err := strconv.Atoi(args[1]); err == nil && n > 0 {
			count = min(n, 20)
		}
	}
	tmpl := g.data.npcTemplate(args[0])
	if tmpl == nil {
		g.adminSay("NPC não encontrado: %s", args[0])
		return
	}
	m := &g.mobs[u.id]
	for range count {
		id := g.emptyNPCMob()
		if id == 0 {
			return
		}
		x, y := int(m.target.X), int(m.target.Y)
		n := g.npcFromTemplate(id, tmpl)
		for s := range 5 {
			n.npc.segX[s], n.npc.segY[s] = x, y
		}
		g.getCurrentScore(id)
		n.info.Player.Status.CurHP = n.info.Player.Status.MaxHP
		if !g.emptyMobGrid(id, &x, &y) {
			*n = Mob{}
			return
		}
		g.placeNPC(id, x, y)
	}
}

func (g *Game) adminUnsupported(value string) {
	g.adminSay("Comando ADM não suportado neste servidor: %s", value)
}

// panelCommand runs a GM command from the admin panel and sends DBSRV the answer.
func (g *Game) panelCommand(pkt protocol.Packet) {
	var p wire.PanelCommand
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	name := wire.GameString(p.CharName[:])
	command := wire.GameString(p.Command[:])
	u := g.users[0]
	var out []string
	if name != "" {
		id := g.userByName(name)
		if id <= 0 {
			out = []string{"Personagem " + name + " não está online neste canal"}
		} else {
			u = g.users[id]
		}
	}
	if len(out) == 0 {
		g.log.Info("panel GM command", "delivery", p.Delivery, "char", name, "command", command)
		out = g.adminCommand(u, command)
		if len(out) == 0 {
			out = []string{"Executado"}
		}
	}
	r := wire.PanelCommandResult{Header: wire.NewHeader(wire.OpPanelCommandResult, 0), Delivery: p.Delivery}
	wire.PutGameString(r.Text[:255], strings.Join(out, " | "))
	g.sendDB(&r)
}

const gmMessageCooldown = 10 * time.Second

// gmMessage is /gm: the player writes to the staff, who read it in the panel.
func (g *Game) gmMessage(u *User, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		g.clientMessage(u, "Use /gm mensagem para falar com a equipe.")
		return
	}
	if time.Since(u.lastGMMessage) < gmMessageCooldown {
		g.clientMessage(u, "Aguarde um pouco para enviar outra mensagem.")
		return
	}
	u.lastGMMessage = time.Now()
	p := wire.GMMessage{Header: wire.NewHeader(wire.OpGMMessage, uint16(u.id)), Channel: int32(g.cfg.Channel)}
	copy(p.Username[:15], u.account.Username[:15])
	copy(p.CharName[:15], g.mobs[u.id].info.Player.Name[:15])
	wire.PutGameString(p.Text[:99], text)
	g.sendDB(&p)
	g.clientMessage(u, "Mensagem enviada à equipe.")
}
