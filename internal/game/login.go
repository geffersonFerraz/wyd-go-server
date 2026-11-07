package game

import (
	"fmt"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// clientMinSize is the part of CheckPacket Phase A enforces: packets
// shorter than their struct are ignored instead of read past the end.
var clientMinSize = map[uint16]int{
	wire.OpCliLogin:         120,
	wire.OpCliNumericPass:   32,
	wire.OpCliTrade:         156,
	wire.OpCliUseItem:       36,
	wire.OpCliMotion:        20,
	opMissionAccept:         16,
	opCliCompounder:         84,
	opCliLindy:              84,
	opCliShany:              84,
	opCliOdin:               84,
	opCliEhre:               84,
	opCliAlchemy:            84,
	opCliAylin:              84,
	opCliAgatha:             84,
	opCliTiny:               84,
	opCliCompoNova:          84,
	opCliExtraction:         20,
	opCliSealInfo:           16,
	opCliRecruit:            20,
	opCliRecruitAccept:      52,
	opCliKickMember:         16,
	wire.OpGuildAlly:        20,
	opCliPutOutSeal:         52,
	wire.OpCliPartyInvite:   42,
	wire.OpCliPartyAccept:   30,
	wire.OpCliPartyExit:     14,
	OpCliPKMode:             16,
	wire.OpCliAttack:        attackSize1,
	wire.OpCliAttackOne:     attackSize1,
	wire.OpCliAttackTwo:     attackSize1,
	wire.OpCliOpenShop:      40,
	wire.OpCliBuyShop:       24,
	wire.OpCliSellShop:      20,
	wire.OpCliGoldToInv:     16,
	wire.OpCliGoldToStorage: 16,
	wire.OpCliCreateChar:    36,
	wire.OpCliDeleteChar:    44,
	wire.OpCliEnterWorld:    32,
	wire.OpCliChat:          144,
	wire.OpCliCommand:       284,
	wire.OpCliMove:          52,
	wire.OpCliMoveStop:      52,
	wire.OpCliMoveIllus:     52,
	wire.OpCliMoveItem:      20,
	wire.OpCliRequestMob:    16,
}

// handleClient is CUser::PacketControl (Phase A subset).
func (g *Game) handleClient(u *User, pkt protocol.Packet) {
	h := pkt.Header
	if need, ok := clientMinSize[h.PacketID]; ok && int(h.Size) < need {
		g.log.Warn("short client packet", "slot", u.id, "id", fmt.Sprintf("0x%X", h.PacketID), "size", h.Size)
		return
	}
	// The C++ trusted only its own slot id for the sender.
	if h.TimeStamp != 0x0E0A1ACA {
		le16(pkt.Raw[6:], uint16(u.id))
		pkt.Header.ClientID = uint16(u.id)
	}
	u.lastReceiveSec = g.secCounter

	switch h.PacketID {
	case wire.OpCliLogin:
		g.requestLogin(u, pkt)
	case wire.OpCliNumericPass:
		g.requestNumeric(u, pkt)
	case wire.OpCliCreateChar:
		g.requestCreateChar(u, pkt)
	case wire.OpCliDeleteChar:
		g.requestDeleteChar(u, pkt)
	case wire.OpCliEnterWorld:
		g.requestEnterWorld(u, pkt)
	case wire.OpCliLogout:
		g.charLogout(u)
	case wire.OpCliMove, wire.OpCliMoveStop, wire.OpCliMoveIllus:
		g.requestAction(u, pkt)
	case wire.OpCliChat:
		g.requestChat(u, pkt)
	case wire.OpCliMoveItem:
		g.requestMoveItem(u, pkt)
	case wire.OpCliRequestMob:
		g.requestMobInfo(u, pkt)
	case wire.OpCliTeleport:
		g.requestTeleport(u)
	case wire.OpCliOpenShop:
		g.requestOpenShop(u, pkt)
	case wire.OpCliBuyShop:
		g.requestBuyShop(u, pkt)
	case wire.OpCliSellShop:
		g.requestSellShop(u, pkt)
	case wire.OpCliRepurchase:
		g.sendRepurchase(u)
	case wire.OpCliGoldToInv:
		g.requestGoldToInv(u, pkt)
	case wire.OpCliGoldToStorage:
		g.requestGoldToStorage(u, pkt)
	case wire.OpCliAttack, wire.OpCliAttackOne, wire.OpCliAttackTwo:
		g.requestAttack(u, pkt)
	case OpCliPKMode:
		g.requestPKMode(u, pkt)
	case wire.OpCliPartyInvite:
		g.requestAddParty(u, pkt)
	case wire.OpCliPartyAccept:
		g.requestAcceptParty(u, pkt)
	case wire.OpCliPartyExit:
		g.requestExitParty(u, pkt)
	case wire.OpCliUseItem:
		g.requestUseItem(u, pkt)
	case wire.OpCliMotion:
		g.requestMotion(u, pkt)
	case opCliCompounder, opCliLindy, opCliShany, opCliOdin, opCliEhre:
		if g.locked(u, lockCompound, "Bloqueio de composição ativo!") {
			return
		}
		switch h.PacketID {
		case opCliCompounder:
			g.requestCompounder(u, pkt)
		case opCliLindy:
			g.requestLindy(u, pkt)
		case opCliShany:
			g.requestShany(u, pkt)
		case opCliOdin:
			g.requestOdin(u, pkt)
		case opCliEhre:
			g.requestEhre(u, pkt)
		}
	case opCliAlchemy:
		g.requestAlchemy(u, pkt)
	case opCliAylin:
		g.requestAylin(u, pkt)
	case opCliAgatha:
		g.requestAgatha(u, pkt)
	case opCliTiny:
		g.requestTiny(u, pkt)
	case opCliCompoNova:
		g.requestCompoNova(u, pkt)
	case opCliExtraction:
		g.requestExtraction(u, pkt)
	case wire.OpCliCommand:
		g.requestCommand(u, pkt)
	case opDailyRewardCollect:
		g.requestCollectDaily(u, pkt)
	case opMissionAccept:
		g.requestAcceptMission(u, pkt)
	case opMissionCollect:
		g.requestCollectMission(u, pkt)
	case opGroupList:
		g.requestGroupList(u, pkt)
	case opGroupSearch:
		g.requestGroupSearch(u, pkt)
	case opReconnect:
		g.requestReconnectFlag(u, pkt)
	case opCliRecruit:
		g.requestRecruit(u, pkt)
	case opCliRecruitAccept:
		g.requestAcceptRecruit(u, pkt)
	case opCliKickMember:
		g.requestKickMember(u, pkt)
	case wire.OpGuildAlly:
		g.requestAlly(u, pkt)
	case opCliSealInfo:
		g.requestSealInfo(u, pkt)
	case opCliPutOutSeal:
		g.requestPutOutSeal(u, pkt)
	case wire.OpCliResurrect:
		g.requestResurrect(u)
	case wire.OpCliTrade:
		g.requestTrade(u, pkt)
	case wire.OpCliTradeCancel:
		g.cancelTrade(u)
	case opCliAddPoint:
		g.requestAddPoint(u, pkt)
	case opCliSkillBar:
		g.requestChangeSkillBar(u, pkt)
	case opCliDeleteItem:
		g.requestDeleteItem(u, pkt)
	case opCliUngroup:
		g.requestUngroupItem(u, pkt)
	case opCliChangeCity:
		g.requestChangeCity(u, pkt)
	case opCliPickItem:
		g.requestPickItem(u, pkt)
	case opCliDropItem:
		g.requestDropItem(u, pkt)
	case opGate:
		g.requestOpenGate(u, pkt)
	case opCliMerchantNPC:
		g.requestMerchantNPC(u, pkt)
	case opCliLock:
		g.requestLock(u, pkt)
	case opLockStatus:
		g.requestUnlock(u, pkt)
	case opCliBlockPwd:
		g.requestBlockPassword(u)
	case opDuel:
		g.requestDuel(u, pkt)
	case opCliAuctionList:
		g.requestAuctionList(u, pkt)
	case opCliAuctionCreate:
		g.requestAuctionCreate(u, pkt)
	case opCliAuctionOpen:
		g.requestAuctionOpen(u, pkt)
	case opCliAuctionAdd:
		g.requestAuctionAdd(u, pkt)
	case opCliAuctionOwner:
		g.requestAuctionOwner(u, pkt)
	case opCliAuctionBuy:
		g.requestAuctionBuy(u, pkt)
	case opCliAuctionSearch:
		g.requestAuctionSearch(u, pkt)
	case opCliAuctionRemove:
		g.requestAuctionRemove(u, pkt)
	case opCliWPEWarn:
		g.requestWPEWarn(u)
	case wire.OpCliKeepAlive, wire.OpCliIgnored2CB:
	default:
		g.log.Debug("unhandled client packet", "slot", u.id, "id", fmt.Sprintf("0x%X", h.PacketID), "size", h.Size)
	}
}

func le16(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }

// requestLogin is CUser::RequestLogin.
func (g *Game) requestLogin(u *User, pkt protocol.Packet) {
	var p wire.AccountLogin
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	p.Login[15] = 0
	p.Password[11] = 0
	if u.status != userAccept {
		g.clientMessage(u, "Login now, wait a moment.")
		g.flush(u)
		return
	}

	// At most login.max_accounts_per_computer accounts per MAC address.
	same, limit := 0, g.cfg.Login.MaxAccountsPerComputer
	for _, o := range g.users[1:] {
		if o.id != u.id && o.status >= userSelChar && o.mac == p.Mac {
			same++
		}
	}
	if same >= limit {
		g.clientMessage(u, "Limite de %d contas por computador", limit)
		g.flush(u)
		return
	}
	u.mac = p.Mac

	// Quotes and spaces end the login; then upper-case it.
	for i := range p.Login {
		if p.Login[i] == '\'' || p.Login[i] == ' ' {
			p.Login[i] = 0
		}
	}
	login := strings.ToUpper(wire.CString(p.Login[:]))
	wire.PutCString(p.Login[:], login)
	wire.PutCString(u.account.Username[:], login)
	g.log.Info("login attempt", "slot", u.id, "account", login, "ip", u.ip)

	if failedLogins.count(login) >= 3 {
		g.clientMessage(u, "Senha incorreta 3 vezes. Aguarde para tentar novamente.")
		g.flush(u)
		return
	}

	p.Header = wire.NewHeader(wire.OpAccountLogin, uint16(u.id))
	g.sendDB(&p)
	u.status = userLogin
	g.mobs[u.id].mode = 0
}

// requestNumeric is CUser::RequestNumeric.
func (g *Game) requestNumeric(u *User, pkt protocol.Packet) {
	if u.status != userSelChar {
		g.closeUser(u)
		return
	}
	var p wire.NumericPassword
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	p.Header = wire.NewHeader(wire.OpNumericPassword, uint16(u.id))
	if p.RequestChange == 1 && !u.tokenOK {
		// The C++ forwarded this either way; changing the token requires
		// having typed the current one this session.
		p.RequestChange = 0
	}
	g.sendDB(&p)
}

// requestCreateChar is CUser::RequestCreateChar.
func (g *Game) requestCreateChar(u *User, pkt protocol.Packet) {
	var p wire.NewCharacter
	if wire.Unmarshal(pkt.Raw, &p) != nil || p.ClassID > 3 || p.SlotID > 3 || u.status != userSelChar {
		return
	}
	p.Nick[12] = 0
	p.Header = wire.NewHeader(wire.OpNewCharacter, uint16(u.id))
	g.sendDB(&p)
	u.lastPenalty[p.SlotID] = time.Now().Unix()
	u.status = userCreWait
}

// requestDeleteChar is CUser::RequestDeleteChar. The C++ checked the
// equipment and gold of pMob, i.e. the last character played this session,
// not the one being deleted; this checks the slot being deleted (from the
// character list).
func (g *Game) requestDeleteChar(u *User, pkt protocol.Packet) {
	var p wire.DeleteCharacter
	if wire.Unmarshal(pkt.Raw, &p) != nil || p.SlotIndex < 0 || p.SlotIndex > 3 {
		return
	}
	p.Name[15] = 0
	p.Pwd[11] = 0
	if u.status != userSelChar {
		g.clientMessage(u, "Deleting Character, wait a moment")
		return
	}
	for i := 1; i < 15; i++ {
		if id := u.charList.Equip[p.SlotIndex][i].Index; id > 0 && id < MaxItemList {
			g.clientMessage(u, "Desequipe todos os itens do personagem para excluir")
			return
		}
	}
	if u.charList.Gold[p.SlotIndex] != 0 {
		g.clientMessage(u, "Não é possível excluir enquanto possuir gold no inventario")
		return
	}
	if wait := u.lastPenalty[p.SlotIndex] + 7200 - time.Now().Unix(); u.lastPenalty[p.SlotIndex] != 0 && wait > 0 {
		g.clientMessage(u, "Aguarde %d horas e %d minuto(s) para apagar.", wait/3600, (wait%3600)/60)
		return
	}
	u.status = userDelWait
	p.Header = wire.NewHeader(wire.OpDeleteCharacter, uint16(u.id))
	g.sendDB(&p)
}

// requestEnterWorld is the 0x213 case of PacketControl.
func (g *Game) requestEnterWorld(u *User, pkt protocol.Packet) {
	if !u.tokenOK {
		g.signal(u, wire.ClientIDSystem, wire.OpNumericPasswordWrong)
		g.clientMessage(u, "Digite a senha numérica")
		return
	}
	if u.status != userSelChar {
		return
	}
	var p wire.CharacterLogin
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	p.Header = wire.NewHeader(wire.OpCharacterLogin, uint16(u.id))
	g.sendDB(&p)
}

// handleDB is the DB half of PacketControl (Phase A subset).
func (g *Game) handleDB(pkt protocol.Packet) {
	h := pkt.Header
	id := int(h.ClientID)
	var u *User
	if id > 0 && id < MaxPlayer {
		u = g.users[id]
	}

	switch h.PacketID {
	case wire.OpServerHello: // 0x423
		var p wire.Parm2
		if wire.Unmarshal(pkt.Raw, &p) == nil && p.Parm1 == 1 {
			g.sapphire = p.Parm2
		}
	case wire.OpUpdateSapphire: // 0x80E
		var p wire.Parm1
		if wire.Unmarshal(pkt.Raw, &p) == nil {
			if p.Parm1 == 1 {
				g.sapphire <<= 1
			} else {
				g.sapphire >>= 1
			}
			g.sapphire = min(max(g.sapphire, 1), 35)
		}
	case wire.OpCNFChargedList: // 0x428
		var p wire.ChargedGuildList
		if wire.Unmarshal(pkt.Raw, &p) == nil {
			g.chargedGuilds = p.ChargedGuildList
		}
	case wire.OpMessagePanel, wire.OpServerNotice: // 0x101, 0x908
		var p wire.Notice96
		if wire.Unmarshal(pkt.Raw, &p) == nil {
			g.notice(wire.GameString(p.Text[:]))
		}
	case wire.OpCreateGuild, wire.OpAddGuild, wire.OpAddSub, wire.OpGuildAlly, wire.OpGuildWar:
		g.guildDBMessage(pkt)
		return
	case wire.OpBroadcastChat: // 0xD1D: a trumpet from any channel
		g.relayChat(pkt.Raw)
		return
	}
	if u == nil || u.conn == nil && u.status != userSaving4Quit {
		return
	}

	switch h.PacketID {
	case wire.OpCNFAccountLogin: // 0x416
		g.sendCharList(u, pkt)
	case wire.OpAdditionalAccountInfo: // 0x415
		var p wire.AdditionalAccountInfo
		if wire.Unmarshal(pkt.Raw, &p) != nil {
			return
		}
		a := &u.account
		u.accessLevel = p.Access
		a.PassBlock = p.PassBlock
		a.Water = p.Water
		a.Divina, a.Sephira = p.Divina, p.Sephira
		a.Ban, a.BanType = p.Ban, p.BanType
		u.isBanned = p.IsBanned == 1
		a.Cash, a.Honra, a.Novato = uint32(p.Cash), uint32(p.Honra), uint32(p.Novato)
		a.Unique = uint64(p.Unique)
		a.Daily = p.Daily
		a.SingleGift = p.SingleGift
	case wire.OpCNFNewCharacter, wire.OpCNFDeleteCharacter: // 0x418 / 0x419
		var p wire.CharListPacket
		if wire.Unmarshal(pkt.Raw, &p) != nil {
			return
		}
		u.charList = p.CharList
		fixCharListSanc(&p.CharList)
		op := uint16(wire.OpSrvCharListNew)
		if h.PacketID == wire.OpCNFDeleteCharacter {
			op = wire.OpSrvCharListDel
		}
		p.Header = wire.NewHeader(op, wire.ClientIDCharList)
		g.queue(u, &p)
		u.status = userSelChar
	case wire.OpSealInfo: // 3134
		g.sealInfoReply(u, pkt)
	case wire.OpPutInSealSuccess: // 3135
		g.putInSealDone(u)
	case wire.OpItemDelivery: // 0xC0F
		g.itemDelivery(u, pkt)
	case wire.OpCashDelivery: // 0xC0E
		g.cashDelivery(u, pkt)
	case wire.OpBanNotify: // 0xC10
		g.banNotify(u, pkt)
	case wire.OpAdminEdit: // 0xC11
		g.adminEdit(u, pkt)
	case wire.OpNewCharFail: // 0x41D
		g.signal(u, 0, wire.OpSrvCreateFail)
		u.status = userSelChar
	case wire.OpDeleteCharFail: // 0x41E
		g.signal(u, 0, wire.OpSrvDeleteFail)
		u.status = userSelChar
	case wire.OpAlreadyPlaying: // 0x41F (also wrong password on delete)
		g.clientMessage(u, "Senha incorreta.")
		u.status = userSelChar
	case wire.OpDisconnectPrevious: // 0x420
		g.signal(u, wire.ClientIDLogin, wire.OpSrvKickedOther)
		g.flush(u)
		g.closeUser(u)
	case wire.OpAccountNotFound: // 0x421
		g.clientMessage(u, "Não existe uma conta com esse nome.")
		g.signal(u, wire.ClientIDLogin, wire.OpSrvLoginFail)
		g.flush(u)
		g.closeUser(u)
	case wire.OpWrongPassword: // 0x422
		g.clientMessage(u, "Senha incorreta.")
		g.signal(u, wire.ClientIDLogin, wire.OpSrvLoginFail)
		failedLogins.add(wire.CString(u.account.Username[:]))
		g.flush(u)
		g.closeUser(u)
	case wire.OpAccountBlocked: // 0x424
		g.clientMessage(u, "Conta bloqueada.")
		g.flush(u)
		g.closeUser(u)
	case wire.OpAccountDisabled: // 0x425
		g.clientMessage(u, "Conta desativada.")
		g.flush(u)
		g.closeUser(u)
	case 0x426:
		g.clientMessage(u, "Servidor em manutenção. Verifique o site para maiores infos.")
		g.flush(u)
		g.closeUser(u)
	case wire.OpNumericPassword: // 0xFDE: correct
		var p wire.NumericPassword
		if wire.Unmarshal(pkt.Raw, &p) != nil {
			return
		}
		u.tokenOK = true
		p.Header = wire.NewHeader(wire.OpNumericPassword, 0)
		g.queue(u, &p)
	case wire.OpNumericPasswordWrong: // 0xFDF
		u.numericFail++
		if u.numericFail >= 4 {
			g.closeUser(u)
			return
		}
		g.signal(u, wire.ClientIDLogin, wire.OpSrvNumericBad)
		raw := append([]byte(nil), pkt.Raw...)
		le16(raw[6:], 0)
		g.queueRaw(u, raw)
	case wire.OpCNFCharacterLoginExtra: // 0x820 GetCharInfo
		var p wire.CNFCharacterLoginExtra
		if wire.Unmarshal(pkt.Raw, &p) == nil {
			g.mobs[u.id].info = p.Mob
		}
	case wire.OpCNFCharacterLogin: // 0x417
		g.enterWorld(u, pkt)
	case wire.OpSavingQuit: // 0x40A
		var p wire.SavingQuit
		_ = wire.Unmarshal(pkt.Raw, &p)
		if u.status != userPlay && u.status != userSaving4Quit {
			g.sendDB(&wire.Signal{Header: wire.NewHeader(wire.OpNoNeedSave, uint16(u.id))})
		}
		if u.status != userPlay {
			switch p.Mode {
			case 0:
				g.clientMessage(u, "Sua conta foi acessada em outro lugar.")
			case 1:
				g.clientMessage(u, "Conta desativada.")
			}
			g.flush(u)
		}
		g.closeUser(u)
	case wire.OpSaveQuitDone: // 0x40B
		g.mobs[u.id].mode = 0
		u.status = userAccept
		g.closeUser(u)
	}
}

// sendCharList is CUser::SendCharList (0x416 -> client 0x10A).
func (g *Game) sendCharList(u *User, pkt protocol.Packet) {
	var p wire.CNFAccountLogin
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	if wire.CString(p.UserName[:]) != wire.CString(u.account.Username[:]) {
		g.log.Warn("char list for another account", "slot", u.id, "got", wire.CString(p.UserName[:]))
		g.closeUser(u)
		return
	}
	u.charList = p.CharList
	fixCharListSanc(&p.CharList)
	for i := range u.keys {
		u.keys[i] = protocol.KeyTable[rnd()%512]
	}
	p.Keys = u.keys
	u.account.StorageItems = p.Storage
	u.account.StorageCoin = p.GoldStorage
	u.status = userSelChar
	p.Header = wire.NewHeader(wire.OpSrvCharList, wire.ClientIDLogin)
	g.queue(u, &p)
	g.log.Info("account logged in", "slot", u.id, "account", wire.CString(u.account.Username[:]))
}

// fixCharListSanc: with a costume equipped (Equip[12]) the colored refine
// effects 116..125 are shown as plain EF_SANC in the character list.
func fixCharListSanc(cl *wire.CharList) {
	for c := range 4 {
		if cl.Name[c][0] == 0 || cl.Equip[c][12].Index == 0 {
			continue
		}
		for i := 1; i < 11; i++ {
			for e := range 3 {
				if ef := &cl.Equip[c][i].Effect[e]; ef.Index >= 116 && ef.Index <= 125 {
					ef.Index = EF_SANC
					break
				}
			}
		}
	}
}

// notice is SendNotice: a message to every playing user.
func (g *Game) notice(msg string) {
	for _, u := range g.users[1:] {
		if u.status == userPlay {
			g.clientMessage(u, "%s", msg)
		}
	}
}

// failedLogins is FailAccount/CheckFailAccount: three wrong passwords lock
// the login on this server for a while.
var failedLogins = &failTable{m: map[string][]time.Time{}}

type failTable struct{ m map[string][]time.Time }

func (f *failTable) add(login string)       { f.m[login] = append(f.recent(login), time.Now()) }
func (f *failTable) count(login string) int { return len(f.recent(login)) }
func (f *failTable) recent(login string) []time.Time {
	var out []time.Time
	for _, t := range f.m[login] {
		if time.Since(t) < 10*time.Minute {
			out = append(out, t)
		}
	}
	f.m[login] = out
	return out
}
