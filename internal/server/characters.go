package server

import (
	"context"
	"strings"

	"dbsrv/internal/auth"
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// newMob builds a character from a class template (ClearMob + pBaseSet copy).
func (s *Server) newMob(body int) wire.CharInfo {
	var c wire.CharInfo
	c.Player = s.base[body]
	for i := range c.SkillBar {
		c.SkillBar[i] = 0xFF
	}
	c.Player.SkillBar1 = [4]uint8{0xFF, 0xFF, 0xFF, 0xFF}
	return c
}

// handleNewCharacter is _MSG_DBNewCharacter (0x802).
func (s *Server) handleNewCharacter(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.NewCharacter
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	fail := func() { ch.signal(client, wire.OpNewCharFail) }

	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil || p.SlotID > 3 || p.ClassID > 3 {
		fail()
		return
	}
	name := wire.CString(p.Nick[:])
	if !validNewName(name) {
		fail()
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	a := &sess.rec.Account
	// The C++ zeroed the slot before checking it was free, wiping an
	// existing character from memory on a rejected request. Check first.
	if a.Mob[p.SlotID].Player.Name[0] != 0 {
		fail()
		return
	}

	ok, err := s.st.ReserveName(ctx, sess.rec.ID, name, false)
	if err != nil {
		ch.log.Error("reserve name", "err", err)
	}
	if !ok {
		fail()
		return
	}

	c := s.newMob(int(p.ClassID))
	c.Player.Equip[0].Effect[1].Value = 1 // evolution: mortal
	c.Player.Equip[0].Effect[1].Index = uint8(c.Player.Equip[0].Index & 127)
	wire.PutCString(c.Player.Name[:], name)

	old := a.Mob[p.SlotID]
	a.Mob[p.SlotID] = c
	if err := s.st.SaveAccount(ctx, sess.rec); err != nil {
		ch.log.Error("save new character", "err", err)
		a.Mob[p.SlotID] = old
		_ = s.st.ReleaseName(ctx, sess.rec.ID, name)
		fail()
		return
	}
	ch.log.Info("character created", "account", sess.username, "name", name, "class", p.ClassID)
	ch.sendCharList(client, wire.OpCNFNewCharacter, a)
}

// handleNewArch is _MSG_DBNewArch (0x80B): creates an Arch in an empty
// slot, normally carrying its mortal's name.
func (s *Server) handleNewArch(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.NewArch
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	fail := func() { ch.signal(client, wire.OpNewCharFail) }

	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil || p.PosID < 0 || p.PosID > 3 {
		fail()
		return
	}
	name := wire.CString(p.Name[:])
	if name == "" || isDeviceName(name) || strings.Contains(name, "\xA1\xA1") {
		fail()
		return
	}
	var body int
	switch {
	case p.ClassID >= 6 && p.ClassID < 10:
		body = 0
	case p.ClassID >= 16 && p.ClassID < 20:
		body = 1
	case p.ClassID >= 26 && p.ClassID < 30:
		body = 2
	case p.ClassID >= 36 && p.ClassID < 40:
		body = 3
	default:
		fail()
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	a := &sess.rec.Account
	// The C++ zeroed the slot first, so this check could never fail and an
	// occupied slot was overwritten. Enforce the intended rule.
	if a.Mob[p.PosID].Player.Name[0] != 0 {
		fail()
		return
	}
	// The C++ did not reserve Arch names at all. The name is normally the
	// mortal's and already ours; refuse one held by another account.
	if ok, err := s.st.ReserveName(ctx, sess.rec.ID, wire.GameString(p.Name[:]), true); err != nil || !ok {
		if err != nil {
			ch.log.Error("reserve arch name", "err", err)
		}
		fail()
		return
	}

	c := s.newMob(body)
	wire.PutCString(c.Player.Name[:], name) // raw game bytes, as sent
	for i := 1; i < 17; i++ {
		c.Player.Equip[i] = wire.Item{}
	}
	c.Player.Inventory = [64]wire.Item{}
	c.Player.Equip[0].Index = uint16(p.ClassID)
	c.Player.Equip[0].Effect[1] = wire.ItemEffect{Index: uint8(p.ClassID), Value: 2} // evolution: arch
	c.MortalSlot = p.MortalSlot
	c.Player.ClassInfo = uint8(p.ClassInfo)

	old := a.Mob[p.PosID]
	a.Mob[p.PosID] = c
	if err := s.st.SaveAccount(ctx, sess.rec); err != nil {
		ch.log.Error("save new arch", "err", err)
		a.Mob[p.PosID] = old
		fail()
		return
	}
	ch.log.Info("arch created", "account", sess.username, "name", name, "class", p.ClassID)
	ch.sendCharList(client, wire.OpCNFNewCharacter, a)
}

// handleDeleteCharacter is _MSG_DBDeleteCharacter (0x809).
func (s *Server) handleDeleteCharacter(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.DeleteCharacter
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil || p.SlotIndex < 0 || p.SlotIndex > 3 {
		ch.signal(client, wire.OpDeleteCharFail)
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	a := &sess.rec.Account
	m := &a.Mob[p.SlotIndex].Player
	if m.Equip[0].Effect[1].Value >= 3 { // celestials cannot be deleted
		ch.signal(client, wire.OpDeleteCharFail)
		return
	}
	if ok, _ := auth.Verify(sess.rec.PasswordHash, wire.CString(p.Pwd[:])); !ok {
		ch.signal(client, wire.OpAlreadyPlaying) // C++ used 0x41F for a wrong password here
		return
	}
	if m.Name[0] == 0 {
		return
	}

	name := wire.GameString(m.Name[:])
	old := a.Mob[p.SlotIndex]
	a.Mob[p.SlotIndex] = wire.CharInfo{}
	if err := s.st.SaveAccount(ctx, sess.rec); err != nil {
		ch.log.Error("save after delete", "err", err)
		a.Mob[p.SlotIndex] = old
		ch.signal(client, wire.OpDeleteCharFail)
		return
	}
	// The C++ always deleted the char/ file, freeing the name even while an
	// Arch in another slot still used it.
	if !nameInUse(a, name) {
		if err := s.st.ReleaseName(ctx, sess.rec.ID, name); err != nil {
			ch.log.Error("release name", "err", err)
		}
	}
	ch.log.Info("character deleted", "account", sess.username, "name", name)
	ch.sendCharList(client, wire.OpCNFDeleteCharacter, a)
}

func nameInUse(a *wire.Account, name string) bool {
	for i := range a.Mob {
		if strings.EqualFold(wire.GameString(a.Mob[i].Player.Name[:]), name) {
			return true
		}
	}
	return false
}

// handleCreateSubCele is _MSG_DBRequestCreateSubCele (0x830). Like the C++
// it only changes the in-memory account; TMSRV's next save carries it.
func (s *Server) handleCreateSubCele(_ context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.CreateSubCele
	if !decode(ch, pkt, &p) {
		return
	}
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil || p.CharPos > 3 || p.ClassInfo > 3 {
		return
	}
	var baseFace uint32
	switch {
	case p.Face < 10:
		baseFace = 6
	case p.Face < 20:
		baseFace = 16
	case p.Face < 30:
		baseFace = 26
	case p.Face < 40:
		baseFace = 36
	}
	var learn uint32
	for i := 0; i < 8; i++ {
		if bit := uint32(1) << (24 + i); 24+i != 30 && p.Learn&bit != 0 {
			learn |= bit
		}
	}
	learn |= 1 << 30

	sess.mu.Lock()
	defer sess.mu.Unlock()
	sub := &sess.rec.Account.Mob[p.CharPos].Sub
	face := uint16(baseFace + p.ClassInfo)
	sub.Equip = [2]wire.Item{{Index: face, Effect: [3]wire.ItemEffect{{}, {Index: uint8(face), Value: 4}, {}}}}
	sub.Status = 1
	sub.Exp = 0
	sub.Info = 0
	sub.Learn = learn
	sub.Soul = 0
	sub.SubStatus = s.base[p.ClassInfo].BStatus
	for i := range sub.SkillBar {
		sub.SkillBar[i] = 0xFF
	}
}

// numericKey mirrors strncmp(..., 6): only the first 6 characters count.
func numericKey(s string) string {
	if len(s) > 6 {
		return s[:6]
	}
	return s
}

// handleNumericPassword is _MSG_DBRequestNumericPassword (0xFDE).
func (s *Server) handleNumericPassword(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.NumericPassword
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil {
		return
	}
	ok := wire.NumericPassword{Header: wire.NewHeader(wire.OpNumericPassword, client)}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.master {
		ch.send(&ok)
		return
	}
	num := numericKey(wire.CString(p.Num[:]))
	if sess.rec.NumericPassHash == "" || p.RequestChange == 1 {
		if num == "" {
			ch.signal(client, wire.OpNumericPasswordWrong)
			return
		}
		h, err := auth.Hash(num)
		if err == nil {
			err = s.st.SetNumericPassHash(ctx, sess.rec.ID, h)
		}
		if err != nil {
			ch.log.Error("set numeric password", "err", err)
			ch.signal(client, wire.OpNumericPasswordWrong)
			return
		}
		sess.rec.NumericPassHash = h
		ch.send(&ok)
		return
	}
	if good, _ := auth.Verify(sess.rec.NumericPassHash, num); good {
		ch.send(&ok)
		return
	}
	ch.signal(client, wire.OpNumericPasswordWrong)
}
