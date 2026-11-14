package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// runJobs replaces ProcessSecTimer: delivery queue, weekly Kefra respawn
// and the online-count file. (The C++ timer also wrote an empty account to
// account/etc/.xml every second; that is gone.)
func (s *Server) runJobs(ctx context.Context) {
	deliveries := time.NewTicker(s.cfg.DeliveryInterval)
	defer deliveries.Stop()
	clock := time.NewTicker(time.Second)
	defer clock.Stop()
	var lastKefra, lastStatus, lastBeat time.Time
	beat := func() {
		if err := s.st.Heartbeat(ctx, store.DeliveryProtocol); err != nil && ctx.Err() == nil {
			s.log.Warn("heartbeat", "err", err)
		}
		if err := s.st.ReportChannels(ctx, s.channelCounts()); err != nil && ctx.Err() == nil {
			s.log.Warn("channel status", "err", err)
		}
	}
	beat()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deliveries.C:
			s.processDeliveries(ctx)
		case now := <-clock.C:
			now = now.In(s.cfg.Loc)
			if now.Sub(lastBeat) >= 5*time.Second {
				lastBeat = now
				beat()
			}
			// Wednesday 19:00: Kefra respawns on every channel.
			if now.Weekday() == time.Wednesday && now.Hour() == 19 && now.Minute() == 0 &&
				now.Sub(lastKefra) > time.Hour {
				lastKefra = now
				s.resetKefra(ctx)
			}
			if s.cfg.StatusFile != "" && now.Sub(lastStatus) >= 30*time.Second {
				lastStatus = now
				s.writeStatusFile()
			}
		}
	}
}

// writeStatusFile mirrors UpdateConnection (serv%02d.htm): one number per
// server group slot, -1 when that channel is not connected. The C++ wrote
// count*4/3 + rand()%3; this writes the real count.
func (s *Server) writeStatusFile() {
	var b strings.Builder
	for _, n := range s.channelCounts() {
		fmt.Fprintf(&b, "%d ", n)
	}
	tmp := s.cfg.StatusFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err == nil {
		err = os.Rename(tmp, s.cfg.StatusFile)
		if err != nil {
			s.log.Warn("status file", "err", err)
		}
	} else {
		s.log.Warn("status file", "err", err)
	}
}

// channelCounts returns, for each server group slot, how many accounts are
// online on that channel, or -1 when its TMSRV is not connected.
func (s *Server) channelCounts() []int {
	out := make([]int, maxServerGroup)
	for i := range out {
		if i < len(s.cfg.ChannelIPs) && s.channel(i) != nil {
			out[i] = s.sess.count(i)
		} else {
			out[i] = -1
		}
	}
	return out
}

// ValidNumericPassword reports whether s is usable as the numeric second
// password: 1 to 6 digits (only the first 6 characters were ever compared).
func ValidNumericPassword(s string) bool {
	if len(s) == 0 || len(s) > 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Delivery results.
const (
	resOK            = "ok"
	resSentOnline    = "sent_online"
	resNotFound      = "not_found"
	resExists        = "exists"
	resNoSpace       = "no_space"
	resInvalid       = "invalid"
	resNotOnline     = "not_online"
	waitOffline      = "waiting_offline"
	waitCharSelected = "waiting_character"
	// waitOtherServer: the account is playing on another server and the
	// change is to this server's world, so it waits for the player to leave.
	waitOtherServer = "waiting_other_server"
)

// errMoved: the delivery now belongs to the DBSRV of another server.
var errMoved = errors.New("delivery moved to another server")

// leaseChannel marks an online_accounts row held by a delivery, not a player.
const leaseChannel = -1

// withStored runs fn with the stored account of user reserved for this
// DBSRV: nobody is logged into it here (as withOffline) nor on another
// server, and nobody can log in until fn returns. It returns the session
// when the account is online here, or the server that holds it (elsewhere
// > 0) when it is playing on another one; fn does not run in either case.
func (s *Server) withStored(ctx context.Context, user string, fn func()) (online *session, elsewhere int, err error) {
	online = s.sess.withOffline(user, func() {
		held, heldBy, cerr := s.st.ClaimOnline(ctx, user, leaseChannel, 0)
		if cerr != nil {
			err = cerr
			return
		}
		if !held && heldBy != 0 {
			elsewhere = heldBy
			return
		}
		if held {
			defer func() {
				if rerr := s.st.SetOffline(context.WithoutCancel(ctx), user, leaseChannel, 0); rerr != nil {
					s.log.Error("release account", "account", user, "err", rerr)
				}
			}()
		}
		fn() // !held: no such account; fn reports it
	})
	return online, elsewhere, err
}

// handOver gives a delivery to the DBSRV of the server where the account is
// playing: only it can apply the change to the live account.
func (s *Server) handOver(ctx context.Context, id int64, server int) (string, bool, error) {
	if err := s.st.MoveDelivery(ctx, id, server, fmt.Sprintf("moved_to_server: %d", server)); err != nil {
		return "", false, err
	}
	return "", false, errMoved
}

func (s *Server) processDeliveries(ctx context.Context) {
	list, err := s.st.PendingDeliveries(ctx, 100)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("pending deliveries", "err", err)
		}
		return
	}
	for _, d := range list {
		result, done, err := s.deliver(ctx, d)
		log := s.log.With("delivery", d.ID, "kind", d.Kind, "account", d.Username)
		if errors.Is(err, errMoved) {
			log.Info("delivery handed to the server where the account is playing")
			continue // no longer ours: its row must not be touched again
		}
		if err != nil {
			log.Error("delivery failed, will retry", "err", err)
			_ = s.st.DeferDelivery(ctx, d.ID, "error: "+err.Error())
			continue
		}
		if !done {
			if d.Attempts == 0 {
				log.Info("delivery waiting", "reason", result)
			}
			_ = s.st.DeferDelivery(ctx, d.ID, result)
			continue
		}
		if err := s.st.FinishDelivery(ctx, d.ID, result); err != nil {
			log.Error("finish delivery", "err", err)
			continue
		}
		log.Info("delivery done", "result", result)
	}
}

type deliveryPayload struct {
	Password     string     `json:"password"`
	Numeric      *string    `json:"numeric"`
	PasswordHash string     `json:"password_hash"`
	NumericHash  string     `json:"numeric_hash"`
	Access       int        `json:"access"`
	Type         int        `json:"type"`
	Until        *time.Time `json:"until"`
	ItemID       int        `json:"item_id"`
	Effects      [][2]int   `json:"effects"`
	Amount       int64      `json:"amount"`
	// AfterLogout (item): put it in the stored storage only once the player
	// is offline (TMSRV returned it for lack of room).
	AfterLogout bool `json:"after_logout"`
	// Text (notice): the message for every channel.
	Text string `json:"text"`
	// Channel, Char and Command (gm_command): a GM command from the panel.
	Channel int    `json:"channel"`
	Char    string `json:"char_name"`
	Command string `json:"command"`
}

// secrets returns argon2id hashes for the password and numeric password in
// the payload. Each may be given in clear ("password", "numeric") or already
// hashed ("password_hash", "numeric_hash"; the admin panel does this so no
// plain text is ever stored in the table). bad describes invalid input.
func (p *deliveryPayload) secrets() (hash, num *string, bad string, err error) {
	pick := func(plain *string, hashed string, valid func(string) bool, what string) (*string, string, error) {
		switch {
		case hashed != "":
			if !strings.HasPrefix(hashed, "$argon2id$") {
				return nil, what + "_hash is not an argon2id hash", nil
			}
			return &hashed, "", nil
		case plain != nil:
			if !valid(*plain) {
				return nil, "invalid " + what, nil
			}
			h, err := auth.Hash(*plain)
			return &h, "", err
		}
		return nil, "", nil
	}
	var plainPw *string
	if p.Password != "" {
		plainPw = &p.Password
	}
	if hash, bad, err = pick(plainPw, p.PasswordHash, ValidPassword, "password"); bad != "" || err != nil {
		return nil, nil, bad, err
	}
	num, bad, err = pick(p.Numeric, p.NumericHash, ValidNumericPassword, "numeric")
	return hash, num, bad, err
}

// NoticeUser is the username of notice deliveries (they are for everyone).
const NoticeUser = "*"

// MaxNoticeLen is the longest notice in game bytes (Notice96 keeps a NUL).
const MaxNoticeLen = 95

// deliverNotice sends a staff notice to every connected channel, which
// shows it to all players (as a 0x908 from another channel). A notice is
// only worth it now: with no channel up it is dropped.
func (s *Server) deliverNotice(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 4*MaxNoticeLen {
		return resInvalid + ": text"
	}
	out := wire.Notice96{Header: wire.NewHeader(wire.OpServerNotice, 0)}
	wire.PutGameString(out.Text[:MaxNoticeLen], text)
	sent := 0
	for i := range s.cfg.ChannelIPs {
		if c := s.channel(i); c != nil {
			out.Header = wire.NewHeader(wire.OpServerNotice, uint16(i))
			c.send(&out)
			sent++
		}
	}
	if sent == 0 {
		return resNotOnline
	}
	return fmt.Sprintf("%s: %d", resSentOnline, sent)
}

// ValidPassword: the client sends at most 11 characters (char Password[12]).
func ValidPassword(s string) bool { return len(s) >= 1 && len(s) <= 11 }

// deliver handles one queued delivery. done=false leaves it pending.
func (s *Server) deliver(ctx context.Context, d store.Delivery) (result string, done bool, err error) {
	var p deliveryPayload
	if err := json.Unmarshal(d.Payload, &p); err != nil {
		return resInvalid + ": " + err.Error(), true, nil
	}
	if d.Kind == "notice" {
		return s.deliverNotice(p.Text), true, nil
	}
	if d.Kind == "gm_command" {
		return s.deliverGMCommand(d.ID, p.Channel, p.Char, p.Command), true, nil
	}
	user := d.Username
	if user == "" || len(user) > 15 || strings.ContainsAny(user, " '\"\\/") {
		return resInvalid + ": username", true, nil
	}

	switch d.Kind {
	case "create_account":
		hash, num, bad, err := p.secrets()
		if err != nil {
			return "", false, err
		}
		if bad != "" {
			return resInvalid + ": " + bad, true, nil
		}
		if hash == nil {
			return resInvalid + ": password required", true, nil
		}
		created, err := s.st.CreateAccount(ctx, user, *hash, num, p.Access)
		if err != nil {
			return "", false, err
		}
		if !created {
			return resExists, true, nil
		}
		return resOK, true, nil

	case "password":
		// Either or both of password and numeric ("token") may change.
		hash, num, bad, err := p.secrets()
		if err != nil {
			return "", false, err
		}
		if bad != "" {
			return resInvalid + ": " + bad, true, nil
		}
		if hash == nil && num == nil {
			return resInvalid + ": nothing to change", true, nil
		}
		var found bool
		online, elsewhere, lerr := s.withStored(ctx, user, func() { found, err = s.st.SetPassword(ctx, user, hash, num) })
		if lerr != nil {
			return "", false, lerr
		}
		if elsewhere != 0 {
			return s.handOver(ctx, d.ID, elsewhere)
		}
		if online != nil {
			return waitOffline, false, nil // the C++ also waited until logout
		}
		if err != nil {
			return "", false, err
		}
		if !found {
			return resNotFound, true, nil
		}
		return resOK, true, nil

	case "ban":
		var until *time.Time
		if p.Type != 0 {
			if p.Until == nil {
				return resInvalid + ": until required", true, nil
			}
			until = p.Until
		}
		var found bool
		online, elsewhere, lerr := s.withStored(ctx, user, func() { found, err = s.st.SetBan(ctx, user, p.Type, until) })
		if lerr != nil {
			return "", false, lerr
		}
		if elsewhere != 0 {
			return s.handOver(ctx, d.ID, elsewhere)
		}
		if online != nil {
			date := wire.ItemDate{}
			if until != nil {
				l := until.In(s.cfg.Loc)
				date = wire.ItemDate{Dia: uint8(l.Day()), Mes: uint8(l.Month()), Ano: uint16(l.Year()),
					Hora: uint8(l.Hour()), Minuto: uint8(l.Minute()), Segundo: uint8(l.Second())}
			}
			online.mu.Lock()
			online.rec.Account.BanType = uint32(p.Type)
			online.rec.Account.Ban = date
			online.mu.Unlock()
			if _, err := s.st.SetBan(ctx, user, p.Type, until); err != nil {
				return "", false, err
			}
			// TMSRV applies it (and kicks if needed) and saves it back.
			if ch := s.channel(online.key.Channel); ch != nil {
				out := wire.BanNotify{Header: wire.NewHeader(wire.OpBanNotify, online.key.Client), BanType: int32(p.Type), Ban: date}
				wire.PutCString(out.Username[:], user)
				ch.send(&out)
			}
			return resSentOnline, true, nil
		}
		if err != nil {
			return "", false, err
		}
		if !found {
			return resNotFound, true, nil
		}
		return resOK, true, nil

	case "item":
		if p.ItemID <= 0 || p.ItemID > 65535 || len(p.Effects) > 3 {
			return resInvalid + ": item", true, nil
		}
		it := wire.Item{Index: uint16(p.ItemID)}
		for i, e := range p.Effects {
			if e[0] < 0 || e[0] > 255 || e[1] < 0 || e[1] > 65535 {
				return resInvalid + ": effect out of range", true, nil
			}
			it.Effect[i] = wire.ItemEffect{Index: uint8(e[0]), Value: uint16(e[1])}
		}
		// The storage is this server's: it can be written while the account
		// plays on another server, which never touches it.
		slot := -1
		online := s.sess.withOffline(user, func() { slot, err = s.st.PutInStorage(ctx, user, it) })
		if online != nil {
			if p.AfterLogout {
				return waitOffline, false, nil
			}
			online.mu.Lock()
			inGame := online.slot >= 0
			online.mu.Unlock()
			if !inGame {
				// At character selection TMSRV would overwrite storage on its
				// next save; the C++ wrote the file anyway and lost the item.
				return waitCharSelected, false, nil
			}
			ch := s.channel(online.key.Channel)
			if ch == nil {
				return waitOffline, false, nil
			}
			out := wire.ItemDelivery{Header: wire.NewHeader(wire.OpItemDelivery, online.key.Client), Item: it}
			wire.PutCString(out.Username[:], user)
			ch.send(&out)
			return resSentOnline, true, nil
		}
		if errors.Is(err, store.ErrNotFound) {
			return resNotFound, true, nil
		}
		if err != nil {
			return "", false, err
		}
		if slot < 0 {
			return resNoSpace, true, nil
		}
		return fmt.Sprintf("%s: storage slot %d", resOK, slot), true, nil

	case "kick":
		// Save and disconnect, so the account can be edited safely. Mode 2
		// makes TMSRV close the player without the "account used elsewhere"
		// (0) or "account disabled" (1) message.
		online := s.sess.byName(user)
		if online == nil {
			if where, err := s.st.Online(ctx, user); err != nil {
				return "", false, err
			} else if where != nil && where.Server != s.st.Server() {
				return s.handOver(ctx, d.ID, where.Server)
			}
			return resNotOnline, true, nil
		}
		s.sendSavingQuit(online, 2)
		return resSentOnline, true, nil

	case "edit":
		return s.deliverEdit(ctx, d.ID, user, d.Payload)

	case "login_unblock":
		// The lock lives in each channel's memory: tell all of them.
		out := wire.LoginUnblock{}
		wire.PutCString(out.Username[:], user)
		sent := 0
		for i := range s.cfg.ChannelIPs {
			if c := s.channel(i); c != nil {
				out.Header = wire.NewHeader(wire.OpLoginUnblock, 0)
				c.send(&out)
				sent++
			}
		}
		if sent == 0 {
			return resNotOnline, true, nil
		}
		return fmt.Sprintf("%s: %d", resSentOnline, sent), true, nil

	case "cash":
		if p.Amount <= 0 || p.Amount > 1<<31-1 {
			return resInvalid + ": amount", true, nil
		}
		var found bool
		online, elsewhere, lerr := s.withStored(ctx, user, func() { found, err = s.st.AddCash(ctx, user, p.Amount) })
		if lerr != nil {
			return "", false, lerr
		}
		if elsewhere != 0 {
			return s.handOver(ctx, d.ID, elsewhere)
		}
		if online != nil {
			ch := s.channel(online.key.Channel)
			if ch == nil {
				return waitOffline, false, nil
			}
			out := wire.CashDelivery{Header: wire.NewHeader(wire.OpCashDelivery, online.key.Client), Cash: int32(p.Amount)}
			wire.PutCString(out.Username[:], user)
			ch.send(&out)
			return resSentOnline, true, nil
		}
		if err != nil {
			return "", false, err
		}
		if !found {
			return resNotFound, true, nil
		}
		return resOK, true, nil
	}
	return resInvalid + ": kind", true, nil
}
