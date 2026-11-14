package server

import (
	"context"
	"errors"
	"fmt"
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

	// Channel change (0x814 -> 0x52A): the client reconnects to the new
	// channel and sends "*N token" in Unknow with an empty password.
	changeSlot := -1
	if unk := wire.CString(p.Unknow[:]); strings.HasPrefix(unk, "*") {
		slot, ok := s.sess.consumeToken(login, changeTokenOf(unk), ch.idx)
		if !ok {
			log.Warn("channel change token rejected")
			ch.signal(client, wire.OpWrongPassword)
			return
		}
		changeSlot = slot
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

	if !master {
		if changeSlot < 0 {
			ok, err := auth.Verify(rec.PasswordHash, pw)
			if err != nil {
				log.Error("password hash", "err", err)
			}
			if !ok {
				ch.signal(client, wire.OpWrongPassword)
				return
			}
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
	other, ok := s.sess.claim(sess)
	if !ok && changeSlot >= 0 {
		// The old channel's save-and-quit may still be in flight: it was
		// sent before the client was told to reconnect, but on another link.
		for deadline := time.Now().Add(changeWait); !ok && time.Now().Before(deadline); {
			time.Sleep(20 * time.Millisecond)
			other, ok = s.sess.claim(sess)
		}
	}
	if !ok {
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

	// One database serves every server and an account plays on one of them
	// at a time (its cash, ban and password are shared, and a save rewrites
	// them): take the account for this server before reading it.
	held, heldBy, err := s.st.ClaimOnline(ctx, login, key.Channel, int(key.Client))
	if err != nil || !held {
		s.sess.remove(sess)
		if err != nil {
			log.Error("mark online", "err", err)
			ch.signal(client, wire.OpAlreadyPlaying)
			return
		}
		log.Info("account is playing on another server", "server", heldBy)
		if heldBy == 0 || p.Unknow84 == 0 {
			ch.signal(client, wire.OpAlreadyPlaying)
			return
		}
		// Ask the DBSRV of that server to save and log it out; the client
		// retries, as it does for another channel of this server.
		if _, err := s.st.EnqueueTo(ctx, heldBy, "kick", login, struct{}{}, "dbsrv"); err != nil {
			log.Error("kick on the other server", "err", err)
		}
		ch.signal(client, wire.OpDisconnectPrevious)
		return
	}

	// Reload now that we own the account: a save-and-quit from another
	// channel or server may have committed after the first read.
	if rec, err = s.st.LoadAccount(ctx, login); err != nil {
		log.Error("reload account", "err", err)
		s.endSession(sess)
		ch.signal(client, wire.OpAccountNotFound)
		return
	}
	sess.rec = rec
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
	log.Info("login", "master", master, "channel_change", changeSlot >= 0)

	if changeSlot >= 0 {
		if a.Mob[changeSlot].Player.Name[0] == 0 {
			return
		}
		// Straight into the world, where the old channel saved the
		// character (no character list, no numeric password).
		sess.slot = changeSlot
		ch.sendCharacterLogin(client, sess, true, a.Position)
	}
}

// changeWait bounds how long a channel-change login waits for the old
// channel's save-and-quit.
const changeWait = 5 * time.Second

// changeTokenOf returns the token of a "*N token" channel-change key.
func changeTokenOf(unk string) string {
	f := strings.Fields(unk)
	if len(f) != 2 {
		return ""
	}
	return f[1]
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
	ch.sendCharacterLogin(pkt.Header.ClientID, sess, false, wire.Position{})
}

// handleNoNeedSave is _MSG_DBNoNeedSave (0x805): logout without saving.
func (s *Server) handleNoNeedSave(_ context.Context, ch *channel, pkt protocol.Packet) {
	if sess := s.sess.get(keyOf(ch, pkt)); sess != nil {
		s.endSession(sess)
		ch.log.Info("logout (no save)", "client", pkt.Header.ClientID, "account", sess.username)
	}
}

// handleServerChange is 0x814: a player asks to move to channel
// NewServerID. The C++ answered 0x52A carrying the plain password so TMSRV
// could replay it (and noted the feature never worked). Here 0x52A carries,
// in Unknow, the key "*N token" the client sends back from the new channel
// in place of the password; the token is single-use, short-lived and only
// valid on channel N. An empty Unknow means the change was refused.
func (s *Server) handleServerChange(_ context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.ServerChange
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	if client == 0 || client >= MaxClients {
		return
	}
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil {
		return
	}
	sess.mu.Lock()
	slot := sess.slot
	sess.mu.Unlock()

	reply := wire.AccountLogin{Header: wire.NewHeader(wire.OpReLoginForChange, client)}
	wire.PutCString(reply.Login[:], sess.username)
	n := int(p.NewServerID)
	target := n - 1
	log := ch.log.With("client", client, "account", sess.username, "to", n)
	switch {
	case n < 1 || n > len(s.cfg.ChannelIPs):
		log.Warn("channel change refused: no such channel")
	case target == ch.idx:
		log.Info("channel change refused: already there")
	case s.channel(target) == nil:
		log.Info("channel change refused: channel offline")
	case slot < 0 || slot > 3:
		log.Warn("channel change refused: not playing")
	default:
		tok := s.sess.issueToken(sess.username, target, slot, s.cfg.ChangeServerTokenTTL)
		wire.PutCString(reply.Unknow[:], fmt.Sprintf("*%d %s", n, tok))
		log.Info("channel change")
	}
	ch.send(&reply)
}
