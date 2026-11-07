package server

import (
	"context"
	"encoding/json"
	"errors"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/store"
)

// deliverEdit applies an "edit" delivery (package acctedit):
//   - offline: straight into the stored account;
//   - online, in game with the edited character (or any character for
//     storage, storage gold and access): sent to TMSRV (0xC11), which
//     applies it live and saves;
//   - online on another character or at character selection: a character
//     edit goes into DBSRV's copy of the account (TMSRV only saves the
//     character in play); a storage edit waits for the player to enter the
//     game (TMSRV would overwrite the storage on its next save);
//   - access level: always written to the database and DBSRV's copy too
//     (TMSRV never saves it), and sent to TMSRV when in game.
func (s *Server) deliverEdit(ctx context.Context, user string, raw json.RawMessage) (string, bool, error) {
	var e acctedit.Edit
	if err := json.Unmarshal(raw, &e); err != nil {
		return resInvalid + ": " + err.Error(), true, nil
	}
	if msg := e.Validate(); msg != "" {
		return resInvalid + ": " + msg, true, nil
	}

	if e.Op == acctedit.OpLock {
		return s.deliverLockReload(user, &e)
	}

	var res string
	var err error
	online := s.sess.withOffline(user, func() { res, err = s.editStored(ctx, user, &e) })
	if online == nil {
		if err != nil {
			return "", false, err
		}
		if res == acctedit.ResOK {
			return resOK, true, nil
		}
		return res, true, nil
	}

	online.mu.Lock()
	defer online.mu.Unlock()
	slot := online.slot
	toGame := slot >= 0 && (!e.CharScoped() || slot == e.Char)

	if e.Op == acctedit.OpAccess {
		online.rec.Account.AccessLevel = uint32(e.Value)
		if err := s.st.SaveAccount(ctx, online.rec); err != nil {
			return "", false, err
		}
		if !toGame {
			return resOK, true, nil
		}
	}
	if !toGame {
		if !e.CharScoped() {
			return waitCharSelected, false, nil
		}
		if r := e.Apply(&online.rec.Account); r != acctedit.ResOK {
			return r, true, nil
		}
		if err := s.st.SaveAccount(ctx, online.rec); err != nil {
			return "", false, err
		}
		return resOK, true, nil
	}
	ch := s.channel(online.key.Channel)
	if ch == nil {
		return waitOffline, false, nil
	}
	out := e.Packet(user, online.key.Client)
	ch.send(&out)
	return resSentOnline, true, nil
}

// deliverLockReload forwards an OpLock to TMSRV when the player is in game
// (the lock is loaded on entering the world, so offline or at character
// selection there is nothing to do).
func (s *Server) deliverLockReload(user string, e *acctedit.Edit) (string, bool, error) {
	online := s.sess.withOffline(user, func() {})
	if online == nil {
		return resNotOnline, true, nil
	}
	online.mu.Lock()
	defer online.mu.Unlock()
	if online.slot < 0 {
		return resOK, true, nil
	}
	ch := s.channel(online.key.Channel)
	if ch == nil {
		return resNotOnline, true, nil
	}
	out := e.Packet(user, online.key.Client)
	ch.send(&out)
	return resSentOnline, true, nil
}

// editStored applies e to the stored account of an offline player.
func (s *Server) editStored(ctx context.Context, user string, e *acctedit.Edit) (string, error) {
	rec, err := s.st.LoadAccount(ctx, user)
	if errors.Is(err, store.ErrNotFound) {
		return resNotFound, nil
	}
	if err != nil {
		return "", err
	}
	if r := e.Apply(&rec.Account); r != acctedit.ResOK {
		return r, nil
	}
	return acctedit.ResOK, s.st.SaveAccount(ctx, rec)
}
