package server

import (
	"context"
	"errors"
	"math"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Seal item: index 3443, seal id in EF 59 values (id/256, id%256).
const (
	sealItemIndex  = 3443
	sealItemEffect = 59
)

// sealInfoForClient packs the skills the way ReadSealInfo did: entries of -1
// are skipped and the rest moved to the front, leaving zeros at the end.
func sealInfoForClient(info wire.SealInfo, id int) wire.SealInfo {
	out := info
	out.Status = int32(id)
	out.Skills = [9]int16{}
	n := 0
	for _, sk := range info.Skills {
		if sk != -1 {
			out.Skills[n] = sk
			n++
		}
	}
	return out
}

// handleSealInfo is SealInfoPacket (3134): Value = seal id.
func (s *Server) handleSealInfo(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.SignalValue
	if !decode(ch, pkt, &p) {
		return
	}
	reply := wire.SealInfoReply{Header: wire.NewHeader(wire.OpSealInfo, pkt.Header.ClientID)}
	info, _, err := s.st.Seal(ctx, int(p.Value))
	switch {
	case err == nil:
		reply.Info = sealInfoForClient(info, int(p.Value))
	case errors.Is(err, store.ErrNotFound):
		reply.Info.Status = -1
	default:
		ch.log.Error("read seal", "id", p.Value, "err", err)
		reply.Info.Status = -1
	}
	ch.send(&reply)
}

// handlePutInSeal is PutInSealPacket (3133): turns the selected character
// into a seal item.
func (s *Server) handlePutInSeal(ctx context.Context, ch *channel, pkt protocol.Packet) {
	client := pkt.Header.ClientID
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	slot := sess.slot
	if slot < 0 || slot > 3 { // the C++ did not check this
		return
	}
	a := &sess.rec.Account
	src := a.Mob[slot]
	if src.Player.Name[0] == 0 {
		return
	}
	m := &src.Player

	id, err := s.st.NextSealID(ctx)
	if err != nil {
		ch.log.Error("next seal id", "err", err)
		return
	}

	info := wire.SealInfo{
		Status:    int32(id),
		CON:       sealStat(m.BStatus.CON),
		STR:       sealStat(m.BStatus.STR),
		INT:       sealStat(m.BStatus.INT),
		DEX:       sealStat(m.BStatus.DEX),
		Evolution: int16(m.Equip[0].Effect[1].Value),
		Face:      int16(m.Equip[0].Effect[1].Index),
		Level:     m.BStatus.Level,
	}
	baseSkill := int16(m.ClassInfo) * 24
	info.Skills[0] = -1
	for i := 1; i < 4; i++ {
		if m.Learn[0]&(1<<(i*8-1)) != 0 {
			info.Skills[0] = baseSkill + int16(i*8-1)
		}
	}
	for i := 1; i < 9; i++ {
		info.Skills[i] = -1
		if m.Learn[0]&(1<<(23+i)) != 0 {
			info.Skills[i] = int16(95 + i)
		}
	}

	sealed := src
	if m.Equip[0].Effect[1].Value >= 3 {
		sealed.MortalSlot = -1
	}
	sealed.Escritura = wire.ItemDate{}
	sealed.PesaEnter = 0
	sealed.Player.Inventory[60] = wire.Item{} // bags
	sealed.Player.Inventory[61] = wire.Item{}
	sealed.Player.GuildIndex = 0
	sealed.Player.GuildMemberType = 0
	sealed.Fame = 0
	sealed.HallEnter = 0
	sealed.Revigorante = wire.ItemDate{}
	sealed.Saude = wire.ItemDate{}

	if err := s.st.UpsertSeal(ctx, id, info, sealed); err != nil {
		ch.log.Error("write seal", "id", id, "err", err)
		return
	}

	name := wire.GameString(m.Name[:])
	a.Mob[slot] = wire.CharInfo{}
	if err := s.st.SaveAccount(ctx, sess.rec); err != nil {
		ch.log.Error("save after seal", "err", err)
		a.Mob[slot] = src
		return
	}
	if !nameInUse(a, name) {
		if err := s.st.ReleaseName(ctx, sess.rec.ID, name); err != nil {
			ch.log.Error("release sealed name", "err", err)
		}
	}
	ch.log.Info("character sealed", "account", sess.username, "name", name, "seal", id)

	ch.sendCharList(client, wire.OpCNFDeleteCharacter, a)
	ch.send(&wire.SignalValue{Header: wire.NewHeader(wire.OpPutInSealSuccess, client), Value: int32(slot)})
	item := wire.ItemDelivery{
		// The C++ put the session index (channel*1000+client) here, which is
		// only right on channel 0.
		Header: wire.NewHeader(wire.OpItemDelivery, client),
		Item: wire.Item{Index: sealItemIndex, Effect: [3]wire.ItemEffect{
			{Index: sealItemEffect, Value: uint16(id / 256)},
			{Index: sealItemEffect, Value: uint16(id % 256)},
		}},
	}
	wire.PutCString(item.Username[:], sess.username)
	ch.send(&item)
}

// handleOutSeal is OutSealPacket (3132): creates a character from the seal
// item at Inventory[SrcSlot] of the selected character.
func (s *Server) handleOutSeal(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.PutOutSeal
	if !decode(ch, pkt, &p) {
		return
	}
	client := pkt.Header.ClientID
	fail := func() { ch.signal(client, wire.OpNewCharFail) }
	sess := s.sess.get(keyOf(ch, pkt))
	if sess == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	slot := sess.slot
	// The C++ did not bounds-check SrcSlot before indexing the inventory.
	if slot < 0 || slot > 3 || p.SrcSlot < 0 || p.SrcSlot >= 64 {
		return
	}
	a := &sess.rec.Account
	newSlot := -1
	for i := range a.Mob {
		if a.Mob[i].Player.Name[0] == 0 {
			newSlot = i
			break
		}
	}
	name := wire.CString(p.MobName[:])
	if newSlot == -1 || !validNewName(name) {
		fail()
		return
	}

	item := &a.Mob[slot].Player.Inventory[p.SrcSlot]
	sealID := int(item.Effect[0].Value)*256 + int(item.Effect[1].Value)
	_, sealed, err := s.st.Seal(ctx, sealID)
	if err != nil {
		ch.log.Warn("out seal: read seal", "id", sealID, "err", err)
		return // the C++ answered nothing here either
	}

	if ok, err := s.st.ReserveName(ctx, sess.rec.ID, name, false); err != nil || !ok {
		if err != nil {
			ch.log.Error("reserve name", "err", err)
		}
		fail()
		return
	}

	c := sealed
	c.Player.Gold = 0
	c.Player.GuildIndex = 0
	c.Player.GuildMemberType = 0
	c.LastGuildKickOut = wire.ItemDate{}
	c.Revigorante = wire.ItemDate{}
	c.Divina = wire.ItemDate{}
	c.Sephira = wire.ItemDate{}
	c.RvRPoints = 0
	wire.PutCString(c.Player.Name[:], name)

	oldItem := *item
	a.Mob[newSlot] = c
	*item = wire.Item{}
	if err := s.st.SaveAccount(ctx, sess.rec); err != nil {
		ch.log.Error("save after out seal", "err", err)
		a.Mob[newSlot] = wire.CharInfo{}
		*item = oldItem
		_ = s.st.ReleaseName(ctx, sess.rec.ID, name)
		fail()
		return
	}
	if err := s.st.MarkSealUsed(ctx, sealID); err != nil {
		ch.log.Error("mark seal used", "err", err)
	}
	ch.log.Info("character unsealed", "account", sess.username, "name", name, "seal", sealID)
	ch.sendCharList(client, wire.OpCNFNewCharacter, a)
}

// sealStat fits an attribute into the 32-bit field of the seal tooltip.
func sealStat(v int64) int32 {
	return int32(min(v, math.MaxInt32))
}
