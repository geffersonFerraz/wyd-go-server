package game

import (
	"strconv"
	"strings"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/wire"
)

// adminCommand is HandleAdminCommand ("/gm +comando" or "/sgm +comando").
//
// Fixed:
//   - "/sgm" ran every admin command for anyone, and "/gm" only checked
//     level >= 1000 (which "/@9638" handed out). Both now need an account
//     with AccessLevel > 0 (set in the admin panel); a level >= 1000
//     character is no longer enough, since characters raised to 1010 by
//     "/@9638" in the C++ days would keep GM powers;
//   - sscanf/strncmp quirks made several commands unreachable ("+exp n"
//     compared 8 characters of "+exp", "+generate n" 13, "+equip" 10,
//     "+runes" parsed "+set runes", "+guildhour" parsed "+guildhonour",
//     "+guildchall2" was shadowed by "+guildchall"); the arguments are
//     parsed by words here;
//   - "+dcall" always answered "0 usuarios".
//
// Commands for systems this server does not have (castle week, Kefra,
// weather, drop/exp rates, ban lists, reloads of the C++ text files, fake
// stores, NPC creation by name, renaming) answer that they are not
// supported.
func (g *Game) adminCommand(u *User, value string) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if u.accessLevel <= 0 {
		if pl.BStatus.Level >= 1000 {
			g.log.Warn("GM command refused: level 1000+ character without GM access", "slot", u.id,
				"account", wire.CString(u.account.Username[:]), "char", wire.CString(pl.Name[:]), "command", value)
		}
		return
	}
	if value == "" {
		g.clientMessage(u, "Utilize /adm +command")
		return
	}
	f := strings.Fields(value)
	cmd := f[0]
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
			it.Effect[k] = wire.ItemEffect{Index: uint8(ef), Value: uint8(efv)}
		}
		return it, true
	}
	g.log.Info("admin command", "slot", u.id, "char", g.charName(u.id), "command", value)

	switch cmd {
	case "+item":
		it, ok := itemArgs(0)
		if !ok {
			g.clientMessage(u, "Comando invalido")
			return
		}
		if it.Effect[0].Index == 99 {
			it = wire.Item{Index: it.Index}
			g.setItemBonusLevel(&it, int(pl.Status.Level), 0, 100)
		}
		slot := firstEmptySlot(pl)
		if slot == -1 {
			g.clientMessage(u, "%s", g.lang(nnYouHaveNoSpaceToTrade))
			return
		}
		pl.Inventory[slot] = it
		g.sendItem(u, wire.SlotInv, slot, &it)

	case "+frag":
		x, _ := num(0)
		setTotKill(pl, x)
		g.refreshMob(u.id)
		g.clientMessage(u, "FRAG")

	case "+move":
		x, ok1 := num(0)
		y, ok2 := num(1)
		if !ok1 || !ok2 || !inMap(x, y) {
			g.clientMessage(u, "Coordenadas invalidas")
			return
		}
		g.teleportar(u.id, x, y)

	case "+hpmob":
		hp, ok := num(0)
		name := rest(1)
		if !ok || name == "" {
			g.clientMessage(u, "Comando invalido")
			return
		}
		for i := MaxPlayer; i < MaxMob; i++ {
			o := &g.mobs[i]
			if o.mode == 0 || o.info.Player.Status.CurHP <= 0 || g.charName(i) != name {
				continue
			}
			o.info.Player.Status.CurHP = int32(hp)
			g.refreshMob(i)
			g.clientMessage(u, "HPmob setado")
			return
		}
		g.clientMessage(u, "Mob não encontrado")

	case "+snoop":
		if pl.Info&1 != 0 {
			g.clientMessage(u, "SNOOP OFF")
			pl.Info &^= 1
		} else {
			g.clientMessage(u, "SNOOP ON")
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
		g.clientMessage(u, "Foram desconectados %d usuarios", n)
		g.closeUser(u)

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
		g.clientMessage(u, "Total de Onlines: %d com %d computadores", total, machines)

	case "+exp":
		v, err := strconv.ParseInt(rest(0), 10, 64)
		if err != nil || v < 0 {
			g.clientMessage(u, "Indice invalido")
			return
		}
		pl.Exp = v
		g.sendScore(u.id)
		g.sendEtc(u)

	case "+generate":
		gen, ok := num(0)
		if !ok || gen < 0 || gen >= len(g.data.Geners) {
			g.clientMessage(u, "Indice invalido")
			return
		}
		g.generateMob(gen, 0, 0)
		ge := &g.data.Geners[gen]
		g.clientMessage(u, "Criado o mob %s em %dx %dy", wire.DecodeGame(wire.CString(ge.Leader.Name[:])), ge.SegmentX[0], ge.SegmentY[0])

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
		g.clientMessage(u, "Portao %d %s - %dx %dy", x, word, gi.x, gi.y)

	case "+cash":
		x, ok := num(0)
		if !ok {
			return
		}
		u.account.Cash = uint32(x)
		g.clientMessage(u, "SET CASH")
		g.sendClientInfo(u)

	case "+set":
		if len(args) >= 2 && args[0] == "mission" {
			x, _ := strconv.Atoi(args[1])
			m.info.DailyQuest.QuestID = int32(x)
			m.info.DailyQuest.IsAccepted = false
			g.sendMissionInfo(u)
			g.clientMessage(u, "Setado missão")
			return
		}
		g.adminUnsupported(u, value)

	case "+level":
		x, ok := num(0)
		if !ok || x < 0 || x > 0x7FFF {
			g.clientMessage(u, "Level invalido")
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
			g.clientMessage(u, "Comando inválido")
			return
		}
		pl.Equip[x] = it
		g.sendEtc(u)
		g.sendEquip(u)
		g.getCurrentScore(u.id)
		g.sendItem(u, wire.SlotEquip, x, &pl.Equip[x])

	case "+limparinv":
		for i := range 60 {
			pl.Inventory[i] = wire.Item{}
			g.sendItem(u, wire.SlotInv, i, &pl.Inventory[i])
		}
		g.clientMessage(u, "SET EMPTY CARRY")

	case "+limparbau":
		for i := range u.account.StorageItems {
			u.account.StorageItems[i] = wire.Item{}
			g.sendItem(u, wire.SlotStorage, i, &u.account.StorageItems[i])
		}
		g.clientMessage(u, "SET EMPTY BAU")

	case "+guildowner", "+resetcities":
		// City owners live in the database (gamedata.city_owners) and are
		// managed only in the admin panel; these commands changed only this
		// channel's memory and were undone by the next reload.
		g.clientMessage(u, "Donos de cidade agora são definidos só pelo painel admin (Guildas > Cidades).")

	case "+fame":
		x, ok1 := num(0)
		y, ok2 := num(1)
		if !ok1 || !ok2 || x <= 0 || x >= maxGuild {
			return
		}
		g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: int32(y), GuildIndex: int32(x)})
		g.clientMessage(u, "Fame Enviada com sucesso")

	case "+special":
		x, ok1 := num(0)
		y, ok2 := num(1)
		if !ok1 || !ok2 || x < 0 || x >= 4 || y < 0 || y > 255 {
			g.clientMessage(u, "Comando invalido")
			return
		}
		pl.BStatus.Mastery[x] = uint16(y)
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.clientMessage(u, "SET SPECIAL")

	case "+removeall", "+remove":
		prefix := rest(0)
		if prefix == "" {
			g.clientMessage(u, "Mob não encontrado")
			return
		}
		n := 0
		for i := MaxPlayer; i < MaxMob; i++ {
			if g.mobs[i].mode == 0 || !strings.HasPrefix(g.charName(i), prefix) {
				continue
			}
			x, y := g.mobs[i].target.X, g.mobs[i].target.Y
			g.mobKilled(i, u.id)
			n++
			if cmd == "+remove" {
				g.clientMessage(u, "Mob %s removido de %dx %dy", prefix, x, y)
				return
			}
		}
		switch {
		case n == 0 && cmd == "+remove":
			g.clientMessage(u, "Mob não encontrado")
		case n == 0:
			g.clientMessage(u, "Nao foi encontado mobs")
		default:
			g.clientMessage(u, "Foram removidos %d mobs %s", n, prefix)
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
		g.clientMessage(u, "SET BUFF")

	case "+nobuff":
		m.info.Affects = [len(m.info.Affects)]wire.Affect{}
		g.sendScore(u.id)
		g.getCurrentScore(u.id)
		g.clientMessage(u, "BUFF OFF")
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
		g.clientMessage(u, "Summonado %d personagens...", n)

	case "+summon", "+kick", "+teleport", "+relo", "+ban":
		if len(args) == 0 {
			g.clientMessage(u, "Comando invalido")
			return
		}
		to := g.userByName(args[0])
		if to <= 0 {
			if cmd == "+ban" {
				g.clientMessage(u, "Usuário não está conectado para banir")
			} else {
				g.clientMessage(u, "Usuario não esta conectado")
			}
			return
		}
		switch cmd {
		case "+summon":
			g.teleportar(to, int(m.target.X), int(m.target.Y))
			g.clientMessage(u, "Summonou %s para %dx %dy", g.charName(to), m.target.X, m.target.Y)
		case "+kick":
			g.closeUser(g.users[to])
			g.clientMessage(u, "%s - %s foi kickado - ClientId %d", g.charName(u.id), args[0], to)
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
			g.clientMessage(u, "Usuário banido e desconectado!")
		}

	case "+coin":
		x, ok := num(0)
		if !ok {
			return
		}
		pl.Gold = int32(x)
		g.sendEtc(u)
		g.clientMessage(u, "Gold setado para: %d.", x)

	case "+not":
		msg := rest(0)
		if msg == "" {
			g.clientMessage(u, "Digite uma mensagem")
			return
		}
		g.notice(msg)

	case "+reloadcash":
		g.clientMessage(u, "Loja donate atualizada!")

	case "+status":
		if len(args) == 0 {
			g.clientMessage(u, "Comando invalido")
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
				g.clientMessage(u, "Comando invalido")
				return
			}
			if idx < 0 || idx > 15 {
				g.clientMessage(u, "Indice invalido")
				return
			}
			pl.Equip[idx] = it
			g.sendItem(u, wire.SlotEquip, idx, &it)
			return
		}
		x, _ := num(1)
		switch mode {
		case 0:
			pl.BStatus.STR = uint16(x)
		case 1:
			pl.BStatus.DEX = uint16(x)
		case 2:
			pl.BStatus.INT = uint16(x)
		case 3:
			pl.BStatus.CON = uint16(x)
		}
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendEtc(u)

	case "+learn":
		x, ok := num(0)
		if !ok || x < 0 || x >= 64 {
			g.clientMessage(u, "Comando invalido")
			return
		}
		pl.Learn[x/32] ^= 1 << uint(x%32)
		g.sendEtc(u)
		g.clientMessage(u, "SET LEARN")

	default:
		g.adminUnsupported(u, value)
	}
}

func (g *Game) adminUnsupported(u *User, value string) {
	g.clientMessage(u, "Comando ADM não suportado neste servidor: %s", value)
}
