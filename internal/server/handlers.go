package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

const (
	defaultTokenTTL = 2 * time.Minute
	handlerTimeout  = 15 * time.Second
)

// handle dispatches one packet (CFileDB::ProcessMessage).
func (s *Server) handle(ch *channel, pkt protocol.Packet) {
	ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
	defer cancel()

	switch pkt.Header.PacketID {
	case wire.OpAccountLogin:
		s.handleAccountLogin(ctx, ch, pkt)
	case wire.OpCharacterLogin:
		s.handleCharacterLogin(ctx, ch, pkt)
	case wire.OpNoNeedSave:
		s.handleNoNeedSave(ctx, ch, pkt)
	case wire.OpSaveMob:
		s.handleSave(ctx, ch, pkt, false)
	case wire.OpSaveMobQuit:
		s.handleSave(ctx, ch, pkt, true)
	case wire.OpNewCharacter:
		s.handleNewCharacter(ctx, ch, pkt)
	case wire.OpNewArch:
		s.handleNewArch(ctx, ch, pkt)
	case wire.OpDeleteCharacter:
		s.handleDeleteCharacter(ctx, ch, pkt)
	case wire.OpServerChange:
		s.handleServerChange(ctx, ch, pkt)
	case wire.OpNumericPassword:
		s.handleNumericPassword(ctx, ch, pkt)
	case wire.OpCreateSubCele:
		s.handleCreateSubCele(ctx, ch, pkt)
	case wire.OpStaffMode:
		var p wire.Parm2
		if decode(ch, pkt, &p) && p.Parm1 == 1 {
			ch.staff.Store(p.Parm2)
		}
	case wire.OpNoop0xFAA:
	case wire.OpSealInfo:
		s.handleSealInfo(ctx, ch, pkt)
	case wire.OpPutInSeal:
		s.handlePutInSeal(ctx, ch, pkt)
	case wire.OpOutSeal:
		s.handleOutSeal(ctx, ch, pkt)
	case wire.OpItemDeliveryReturn:
		s.handleItemDeliveryReturn(ctx, ch, pkt)
	case wire.OpSecurityAlert:
		s.handleSecurityAlert(ctx, ch, pkt)
	case wire.OpRankUpdate:
		s.handleRankUpdate(ctx, ch, pkt)
	case wire.OpChannelState:
		s.handleChannelState(ctx, ch, pkt)
	case wire.OpCityOwner:
		s.handleCityOwner(ctx, ch, pkt)
	case wire.OpMacBan:
		s.handleMacBan(ctx, ch, pkt)
	case wire.OpStoreSale:
		s.handleStoreSale(ctx, ch, pkt)
	case wire.OpItemTrackEvent:
		s.handleItemTrackEvent(ctx, ch, pkt)
	case wire.OpItemTrackSync:
		s.handleItemTrackSync(ctx, ch, pkt)
	case wire.OpOpenStore:
		s.handleOpenStore(ctx, ch, pkt)
	case wire.OpGMMessage:
		s.handleGMMessage(ctx, ch, pkt)
	case wire.OpPanelCommandResult:
		s.handlePanelCommandResult(ctx, ch, pkt)
	default:
		if s.handleWorld(ctx, ch, pkt) {
			return
		}
		ch.log.Warn("unhandled packet", "id", fmt.Sprintf("0x%X", pkt.Header.PacketID),
			"client", pkt.Header.ClientID, "size", pkt.Header.Size)
	}
}

// handleItemDeliveryReturn (Go only, 0xC12): TMSRV had no room for an item
// delivery. It is queued again, for the stored storage once the player is
// offline; if even that is full the panel shows "Sem espaço no baú" and the
// delivery stays in the table instead of the item vanishing.
func (s *Server) handleItemDeliveryReturn(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.ItemDelivery
	if !decode(ch, pkt, &p) {
		return
	}
	user := strings.ToUpper(wire.CString(p.Username[:]))
	if user == "" || p.Item.Index == 0 {
		return
	}
	var effects [][2]int
	for _, e := range p.Item.Effect {
		effects = append(effects, [2]int{int(e.Index), int(e.Value)})
	}
	payload := map[string]any{"item_id": int(p.Item.Index), "effects": effects, "after_logout": true}
	id, err := s.st.Enqueue(ctx, "item", user, payload, "tmsrv (baú cheio)")
	if err != nil {
		// Last resort: the log keeps what the item was.
		ch.log.Error("returned item delivery NOT queued", "account", user, "item", p.Item.Index, "effects", effects, "err", err)
		return
	}
	ch.log.Warn("item delivery returned by TMSRV, queued for logout", "account", user, "item", p.Item.Index, "delivery", id)
}

// handleSecurityAlert (Go only, 0xC13) stores a TMSRV alert for the panel.
func (s *Server) handleSecurityAlert(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.SecurityAlert
	if !decode(ch, pkt, &p) {
		return
	}
	kind := fmt.Sprintf("kind %d", p.Kind)
	if p.Kind == wire.AlertWPE {
		kind = store.AlertKindWPE
	}
	a := store.Alert{Kind: kind, Username: wire.CString(p.Username[:]), CharName: wire.GameString(p.CharName[:]),
		IP: wire.CString(p.IP[:]), Channel: int(p.Channel), Detail: wire.GameString(p.Detail[:])}
	id, err := s.st.AddAlert(ctx, a)
	if err != nil {
		ch.log.Error("security alert NOT stored", "kind", a.Kind, "account", a.Username, "char", a.CharName, "ip", a.IP, "err", err)
		return
	}
	ch.log.Warn("security alert", "id", id, "kind", a.Kind, "account", a.Username, "char", a.CharName, "ip", a.IP)
}

// handleRankUpdate (Go only, 0xC14) stores a character's ranking row.
func (s *Server) handleRankUpdate(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.RankUpdate
	if !decode(ch, pkt, &p) {
		return
	}
	e := store.RankEntry{Name: wire.GameString(p.Name[:]), Level: int(p.Level), Class: int(p.Class), Evolution: int(p.Evolution),
		Guild: int(p.Guild), Patent: int(p.Patent), Kingdom: int(p.Kingdom), Frags: int(p.Frags), Power: int(p.Power),
		Fame: int(p.Fame), Citizen: int(p.Citizen), PvPKill: int(p.PvPKill), PvMKill: int(p.PvMKill), Mantle: int(p.Mantle),
		Exp: p.Exp, Equip: p.Equip}
	if e.Name == "" {
		return
	}
	if err := s.st.SaveRank(ctx, e); err != nil && ctx.Err() == nil {
		ch.log.Error("ranking not saved", "char", e.Name, "err", err)
	}
}

func decode(ch *channel, pkt protocol.Packet, v any) bool {
	if err := wire.Unmarshal(pkt.Raw, v); err != nil {
		ch.log.Warn("decode", "id", fmt.Sprintf("0x%X", pkt.Header.PacketID), "err", err)
		return false
	}
	return true
}

func keyOf(ch *channel, pkt protocol.Packet) sessionKey {
	return sessionKey{Channel: ch.idx, Client: pkt.Header.ClientID}
}

// isDeviceName rejects names Windows could not create as files (the C++
// server checked COM0-9 and LPT0-9; CON, PRN, AUX and NUL failed when the
// char/ file was created).
func isDeviceName(name string) bool {
	u := strings.ToUpper(name)
	switch u {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(u) == 4 && (strings.HasPrefix(u, "COM") || strings.HasPrefix(u, "LPT")) && u[3] >= '0' && u[3] <= '9'
}

// IsDeviceName is isDeviceName for other packages (the admin panel).
func IsDeviceName(name string) bool { return isDeviceName(name) }

// validNewName mirrors _MSG_DBNewCharacter: ASCII letters, digits and '-'.
func validNewName(name string) bool {
	if name == "" || isDeviceName(name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c != '-' && !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// charList builds st_CharList (CFileDB::DBGetSelChar). The C++ copied 22
// equip items into a 20-item array, spilling into the next entry and, for
// slot 3, over GuildId and Gold; this copies exactly 20.
func charList(a *wire.Account) wire.CharList {
	var cl wire.CharList
	for i := range a.Mob {
		m := &a.Mob[i].Player
		cl.Name[i] = m.Name
		cl.Equip[i] = m.Equip
		if idx := cl.Equip[i][0].Index; (idx >= 22 && idx <= 25) || idx == 32 {
			cl.Equip[i][0].Index = uint16(cl.Equip[i][0].Effect[1].Index)
		}
		cl.GuildID[i] = m.GuildIndex
		cl.PositionX[i] = int16(m.Last.X)
		cl.PositionY[i] = int16(m.Last.Y)
		cl.Status[i] = m.Status
		cl.Gold[i] = m.Gold
		cl.Exp[i] = m.Exp
	}
	return cl
}

func (ch *channel) sendCharList(client uint16, op uint16, a *wire.Account) {
	ch.send(&wire.CharListPacket{Header: wire.NewHeader(op, client), CharList: charList(a)})
}

// sendCharacterLogin sends 0x820 then 0x417 for the selected slot.
// pos, when not zero, is where the character appears (channel change);
// otherwise TMSRV picks the spawn point.
func (ch *channel) sendCharacterLogin(client uint16, sess *session, withAccountExtras bool, pos wire.Position) {
	a := &sess.rec.Account
	c := &a.Mob[sess.slot]

	extra := wire.CNFCharacterLoginExtra{Header: wire.NewHeader(wire.OpCNFCharacterLoginExtra, client), Mob: *c}
	if withAccountExtras { // only the change-server path filled these in C++
		extra.Cash, extra.Honra, extra.Novato = int32(a.Cash), int32(a.Honra), int32(a.Novato)
		extra.PassBlock = a.PassBlock
	}
	ch.send(&extra)

	login := wire.CNFCharacterLogin{
		Header:    wire.NewHeader(wire.OpCNFCharacterLogin, client),
		WorldPos:  pos,
		Mob:       c.Player,
		SlotIndex: int16(sess.slot),
		Affect:    c.Affects,
	}
	copy(login.SkillBar2[:], c.SkillBar[:])
	ch.send(&login)
	ch.sendItemTracks(client, sess)
}

// sendAdditionalInfo sends 0x415 (CFileDB::SendAdditionalAccountInfo).
func (ch *channel) sendAdditionalInfo(client uint16, sess *session) {
	a := &sess.rec.Account
	p := wire.AdditionalAccountInfo{
		Header:     wire.NewHeader(wire.OpAdditionalAccountInfo, client),
		Access:     int32(a.AccessLevel),
		Unique:     int64(a.Unique),
		Daily:      a.Daily,
		Water:      a.Water,
		Divina:     a.Divina,
		Sephira:    a.Sephira,
		SingleGift: a.SingleGift,
		Ban:        a.Ban,
		BanType:    a.BanType,
		Cash:       int32(a.Cash),
		Honra:      int32(a.Honra),
		Novato:     int32(a.Novato),
		PassBlock:  a.PassBlock,
	}
	if sess.master {
		p.IsBanned = 1
	}
	ch.send(&p)
}

// endSession logs sess out: removes it from the online table in memory and
// in Postgres (online_accounts, used by the SQL edit guard).
func (s *Server) endSession(sess *session) {
	s.sess.remove(sess)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.st.SetOffline(ctx, sess.username, sess.key.Channel, int(sess.key.Client)); err != nil {
		s.log.Error("mark offline", "account", sess.username, "err", err)
	}
}

// sendSavingQuit asks the channel holding sess to save and log it out
// (CFileDB::SendDBSavingQuit). Mode 0 = another login is waiting.
func (s *Server) sendSavingQuit(sess *session, mode int32) {
	ch := s.channel(sess.key.Channel)
	if ch == nil {
		// Its TMSRV is gone; nothing will ever save it, so free the account.
		s.endSession(sess)
		return
	}
	p := wire.SavingQuit{Header: wire.NewHeader(wire.OpSavingQuit, sess.key.Client), Mode: mode}
	wire.PutCString(p.AccountName[:], sess.username)
	ch.send(&p)
}
