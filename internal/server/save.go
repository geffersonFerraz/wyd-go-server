package server

import (
	"context"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// handleSave is _MSG_DBSaveMob (0x807, periodic) and _MSG_DBSaveMobQuit
// (0x806, save then log out).
func (s *Server) handleSave(ctx context.Context, ch *channel, pkt protocol.Packet, quit bool) {
	var p wire.SaveCharacter
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil {
		ch.log.Warn("save for client with no session", "client", client, "quit", quit)
		if quit {
			ch.signal(client, wire.OpSaveQuitDone)
		}
		return
	}
	log := ch.log.With("client", client, "account", sess.username, "quit", quit)

	sess.mu.Lock()
	defer sess.mu.Unlock()

	user := wire.CString(p.User[:])
	slot := sess.slot
	switch {
	case user != sess.username:
		// The C++ checked this only on quit; a periodic save under the wrong
		// name would have overwritten this account with another player's data.
		log.Error("save rejected: account name mismatch", "packet_user", user)
	case slot < 0 || slot > 3 || int(p.CharSlot) != slot:
		log.Error("save rejected: slot mismatch", "session_slot", slot, "packet_slot", p.CharSlot)
	default:
		applySave(&sess.rec.Account, slot, &p, quit)
		if err := s.st.SaveAccount(ctx, sess.rec); err != nil {
			log.Error("SAVE FAILED", "err", err)
		}
	}

	if quit {
		// The C++ also logged out on every rejected save-and-quit.
		s.endSession(sess)
		ch.signal(client, wire.OpSaveQuitDone)
		log.Info("logout")
	}
}

// applySave copies a STRUCT_SAVECHARACTER into the account, field for field
// as CFileDB::ProcessMessage did.
func applySave(a *wire.Account, slot int, p *wire.SaveCharacter, quit bool) {
	c := &a.Mob[slot]
	*c = p.Mob
	copy(c.Player.SkillBar1[:], p.SkillBar[0:4])
	copy(c.SkillBar[:], p.SkillBar[4:20])

	a.StorageItems = p.Storage
	a.StorageCoin = int64(p.Coin)
	a.Insignias = uint64(p.Insignia)
	a.BanType = p.BanType
	a.Ban = p.Ban
	if quit {
		// TMSRV's SaveUser (0x807) never fills Friends; the C++ copied the
		// zeros anyway, wiping the list on every periodic save until quit.
		a.Friends = p.Friends
	}
	a.Cash = uint32(p.Cash)
	a.Honra = uint32(p.Honra)
	a.Novato = uint32(p.Novato)
	a.PassBlock = p.PassBlock
	a.Unique = uint64(p.Unique)
	a.Divina = p.Divina
	a.Sephira = p.Sephira
	a.Daily = p.Daily
	a.Water = p.Water
	a.SingleGift = p.SingleGift
	a.Position = p.Position
	if quit {
		a.CharSlot = -1
	} else {
		a.CharSlot = p.Slot
	}
}
