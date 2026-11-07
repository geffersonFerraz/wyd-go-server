package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// handleAccountLogin is _MSG_DBAccountLogin (0x803).
func (s *Server) handleAccountLogin(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.AccountLogin
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	key := keyOf(ch, pkt)
	login := strings.ToUpper(wire.CString(p.Login[:]))
	pw := wire.CString(p.Password[:])
	log := ch.log.With("client", client, "account", login)

	if login == "" || isDeviceName(login) {
		ch.signal(client, wire.OpAccountNotFound)
		return
	}

	rec, err := s.st.LoadAccount(ctx, login)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Error("load account", "err", err)
		}
		ch.signal(client, wire.OpAccountNotFound)
		return
	}
	a := &rec.Account
	now := time.Now().In(s.cfg.Loc)

	// Admin disable (_MSG_NPDisable) until Year/YearDay, in struct tm units.
	// The C++ compared only the year; this compares year then day as meant.
	if a.Year != 0 && a.YearDay != 0 {
		y, yd := int32(now.Year()-1900), int32(now.YearDay()-1)
		if a.Year > y || (a.Year == y && a.YearDay >= yd) {
			ch.signal(client, wire.OpAccountBlocked)
			return
		}
	}

	// Legacy change-server key: "server enc0..enc7 slot" put in TempKey.
	changeServer := false
	if tk, unk := wire.CString(a.TempKey[:]), wire.CString(p.Unknow[:]); tk != "" && unk != "" {
		if err := s.st.ClearTempKey(ctx, rec.ID); err != nil {
			log.Error("clear temp key", "err", err)
		}
		if !strings.HasPrefix(unk, tk) {
			log.Warn("change-server key mismatch")
			return // C++ sends nothing here either
		}
		changeServer = true
	}

	master := false
	if s.cfg.MasterPasswordHash != "" {
		if ok, _ := auth.Verify(s.cfg.MasterPasswordHash, pw); ok {
			master = true
			log.Warn("master password login")
		}
	}

	if !master && banActive(a, now, s.cfg.Loc) {
		if a.BanType == 2 {
			ch.signal(client, wire.OpAccountDisabled)
		} else {
			ch.signal(client, wire.OpAccountBlocked)
		}
		return
	}

	if !changeServer && !master {
		ok := s.sess.consumeToken(login, pw)
		if !ok {
			ok, err = auth.Verify(rec.PasswordHash, pw)
			if err != nil {
				log.Error("password hash", "err", err)
			}
		}
		if !ok {
			ch.signal(client, wire.OpWrongPassword)
			return
		}
		// The C++ kept these states as a password prefix, so a status
		// account could never pass the comparison above.
		switch rec.Status {
		case auth.StatusBlocked:
			ch.signal(client, wire.OpAccountBlocked)
			return
		case auth.StatusDisabled:
			ch.signal(client, wire.OpAccountDisabled)
			return
		case auth.StatusDefective:
			ch.signal(client, wire.OpAccountNotFound)
			return
		}
	}

	sess := &session{key: key, username: login, slot: -1, master: master, rec: rec}
	if cur := s.sess.get(key); cur != nil && cur.username == login {
		return // C++: idxName == idx, already logged in on this client
	}
	if other, ok := s.sess.claim(sess); !ok {
		if other == nil || p.Unknow84 == 0 { // nil: a delivery is writing it right now

			ch.signal(client, wire.OpAlreadyPlaying)
			return
		}
		// Ask the other channel to save and log it out; the client retries.
		ch.signal(client, wire.OpDisconnectPrevious)
		s.sendSavingQuit(other, 0)
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Reload now that we own the account: a save-and-quit from another
	// channel may have committed after the first read.
	if rec, err = s.st.LoadAccount(ctx, login); err != nil {
		log.Error("reload account", "err", err)
		s.endSession(sess)
		ch.signal(client, wire.OpAccountNotFound)
		return
	}
	sess.rec = rec
	if err := s.st.SetOnline(ctx, login, key.Channel, int(key.Client)); err != nil {
		log.Error("mark online", "err", err) // only the SQL edit guard depends on it
	}
	a = &rec.Account
	if a.BanType != 0 && !banActive(a, now, s.cfg.Loc) {
		a.BanType, a.Ban = 0, wire.ItemDate{} // expired; persisted on the next save
	}
	if swapArchNames(a) {
		if err := s.st.SaveAccount(ctx, rec); err != nil {
			log.Error("save after name swap", "err", err)
		}
	}

	reply := wire.CNFAccountLogin{
		Header:      wire.NewHeader(wire.OpCNFAccountLogin, client),
		CharList:    charList(a),
		Storage:     a.StorageItems,
		GoldStorage: a.StorageCoin,
	}
	wire.PutCString(reply.UserName[:], login)
	ch.send(&reply)
	ch.sendAdditionalInfo(client, sess)
	log.Info("login", "master", master)

	if changeServer {
		// Unknow = "server enc0 .. enc7 slot"
		f := strings.Fields(wire.CString(p.Unknow[:]))
		if len(f) < 10 {
			return
		}
		slot, err := strconv.Atoi(f[9])
		if err != nil || slot < 0 || slot > 3 || a.Mob[slot].Player.Name[0] == 0 {
			return
		}
		sess.slot = slot
		ch.sendCharacterLogin(client, sess, true)
	}
}

// banActive reports whether a timed ban is still running.
func banActive(a *wire.Account, now time.Time, loc *time.Location) bool {
	b := a.Ban
	if a.BanType == 0 || b.Ano == 0 || b.Mes == 0 || b.Dia == 0 {
		return false
	}
	until := time.Date(int(b.Ano), time.Month(b.Mes), int(b.Dia), int(b.Hora), int(b.Minuto), 0, 0, loc)
	return now.Before(until)
}

// swapArchNames implements the name-swap item: if one character wears item
// 774 and another 775 in Equip[13], their names are exchanged and both items
// consumed.
func swapArchNames(a *wire.Account) bool {
	left, right := -1, -1
	for q := range a.Mob {
		switch a.Mob[q].Player.Equip[13].Index {
		case 774:
			left = q
		case 775:
			right = q
		}
	}
	if left == -1 || right == -1 {
		return false
	}
	l, r := &a.Mob[left].Player, &a.Mob[right].Player
	l.Name, r.Name = r.Name, l.Name
	l.Equip[13], r.Equip[13] = wire.Item{}, wire.Item{}
	return true
}

// handleCharacterLogin is _MSG_DBCharacterLogin (0x804).
func (s *Server) handleCharacterLogin(_ context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.CharacterLogin
	if !decode(ch, pkt, &p) {
		return
	}
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil || p.CharIndex < 0 || p.CharIndex > 3 {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.rec.Account.Mob[p.CharIndex].Player.Name[0] == 0 {
		return
	}
	sess.slot = int(p.CharIndex)
	ch.sendCharacterLogin(pkt.Header.ClientID, sess, false)
}

// handleNoNeedSave is _MSG_DBNoNeedSave (0x805): logout without saving.
func (s *Server) handleNoNeedSave(_ context.Context, ch *channel, pkt protocol.Packet) {
	if sess := s.sess.get(keyOf(ch, pkt)); sess != nil {
		s.endSession(sess)
		ch.log.Info("logout (no save)", "client", pkt.Header.ClientID, "account", sess.username)
	}
}

// handleServerChange is 0x814. The C++ answered 0x52A carrying the plain
// password so TMSRV could replay it as 0x803 (and noted the feature never
// worked). Passwords are hashed now, so 0x52A carries a one-time token that
// 0x803 accepts in place of the password.
func (s *Server) handleServerChange(_ context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.ServerChange
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	if client == 0 || client >= MaxClients || p.NewServerID <= 0 || p.NewServerID > MaxChannels {
		return
	}
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil {
		return
	}
	tok := s.sess.issueToken(sess.username, s.cfg.ChangeServerTokenTTL)
	reply := wire.AccountLogin{Header: wire.NewHeader(wire.OpReLoginForChange, client)}
	wire.PutCString(reply.Login[:], sess.username)
	wire.PutCString(reply.Password[:], tok)
	ch.send(&reply)
}
