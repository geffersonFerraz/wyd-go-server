package server

import (
	"context"
	"fmt"
	"sync"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

const (
	maxGuild       = 65535 // MAX_GUILD
	maxServerGroup = 10    // MAX_SERVERGROUP; also "no channel" for Kefra
	donateStores   = 10
	donateSlots    = 27
)

// world is the shared game state the C++ kept in globals (g_pGuild,
// g_pGuildAlly, g_pGuildWar, ChargedGuildList, g_pStore, Kefra). It is
// written through to Postgres, except ChargedGuildList, which every TMSRV
// re-reports and the C++ never stored either.
type world struct {
	mu      sync.Mutex
	guilds  map[int]*store.Guild
	ally    map[int]int
	war     map[int]int
	charged [maxServerGroup][5]int32
	donate  [donateStores][donateSlots]store.DonateSlot
	kefra   store.KefraState
}

// LoadWorld reads guilds, relations, the donate store and Kefra state.
// Call it before Serve.
func (s *Server) LoadWorld(ctx context.Context) error {
	w := &world{guilds: map[int]*store.Guild{}}
	guilds, err := s.st.Guilds(ctx)
	if err != nil {
		return fmt.Errorf("guilds: %w", err)
	}
	for i := range guilds {
		w.guilds[guilds[i].ID] = &guilds[i]
	}
	if w.ally, err = s.st.GuildRelations(ctx, store.RelationAlly); err != nil {
		return err
	}
	if w.war, err = s.st.GuildRelations(ctx, store.RelationWar); err != nil {
		return err
	}
	slots, err := s.st.DonateStore(ctx)
	if err != nil {
		return err
	}
	for _, d := range slots {
		w.donate[d.Store][d.Slot] = d
	}
	if w.kefra, err = s.st.Kefra(ctx); err != nil {
		return err
	}
	s.world = w
	s.log.Info("world loaded", "guilds", len(w.guilds), "donate_slots", len(slots), "kefra_channel", w.kefra.FirstChannel)
	return nil
}

// broadcast sends v to every connected channel.
func (s *Server) broadcast(v any) {
	for i := range s.cfg.ChannelIPs {
		if ch := s.channel(i); ch != nil {
			ch.send(v)
		}
	}
}

// broadcastRaw re-sends a received packet unchanged to every channel
// (the sender included, as the C++ did).
func (s *Server) broadcastRaw(raw []byte) {
	for i := range s.cfg.ChannelIPs {
		if ch := s.channel(i); ch != nil {
			b := append([]byte(nil), raw...)
			if err := ch.conn.SendRaw(b); err != nil {
				ch.log.Warn("broadcast", "err", err)
			}
		}
	}
}

// broadcastParm2 mirrors SendDBSignalParm2 to every channel: a Parm2 body
// sent with sizeof(MSG_STANDARDPARM3).
func (s *Server) broadcastParm2(op uint16, p1, p2 int32) {
	s.broadcast(&wire.Parm3{Header: wire.NewHeader(op, 0), Parm1: p1, Parm2: p2})
}

func (s *Server) handleWorld(ctx context.Context, ch *channel, pkt protocol.Packet) bool {
	w := s.world
	switch pkt.Header.PacketID {
	case wire.OpGuildZoneReport:
		var p wire.GuildZoneReport
		srv := int(pkt.Header.ClientID) // TMSRV puts its server index here
		if !decode(ch, pkt, &p) || srv >= maxServerGroup {
			return true
		}
		w.mu.Lock()
		w.charged[srv] = p.Guild
		out := wire.ChargedGuildList{Header: wire.NewHeader(wire.OpCNFChargedList, 0), ChargedGuildList: w.charged}
		w.mu.Unlock()
		s.broadcast(&out)

	case wire.OpGuildAlly, wire.OpGuildWar:
		var p wire.GuildRelation
		if !decode(ch, pkt, &p) || p.GuildIndex1 == 0 || p.GuildIndex1 >= maxGuild {
			return true
		}
		kind, m := store.RelationAlly, w.ally
		if pkt.Header.PacketID == wire.OpGuildWar {
			kind, m = store.RelationWar, w.war
		}
		w.mu.Lock()
		m[int(p.GuildIndex1)] = int(p.GuildIndex2)
		w.mu.Unlock()
		if err := s.st.SetGuildRelation(ctx, int(p.GuildIndex1), kind, int(p.GuildIndex2)); err != nil {
			ch.log.Error("save guild relation", "err", err)
		}
		s.broadcastParm2(pkt.Header.PacketID, int32(p.GuildIndex1), int32(p.GuildIndex2))

	case wire.OpCreateGuild:
		var p wire.CreateGuild
		if !decode(ch, pkt, &p) || p.GuildID <= 0 || p.GuildID >= maxGuild {
			return true
		}
		w.mu.Lock()
		if g := w.guilds[int(p.GuildID)]; g != nil && g.Name != "" {
			w.mu.Unlock()
			ch.log.Error("create guild: slot is not empty", "guild", p.GuildID, "existing", g.Name)
			return true
		}
		g := &store.Guild{ID: int(p.GuildID), Name: wire.GameString(p.GuildName[:]), Kingdom: int(p.Kingdom), Citizen: int(p.Citizen)}
		w.guilds[g.ID] = g
		saved := *g
		w.mu.Unlock()
		s.saveGuild(ctx, ch, saved)
		s.broadcast(&p)

	case wire.OpAddGuild:
		var p wire.AddGuild
		if !decode(ch, pkt, &p) || p.GuildIndex <= 0 || p.GuildIndex >= maxGuild || p.Type < 0 || p.Type > 3 {
			return true
		}
		w.mu.Lock()
		g := w.guildLocked(int(p.GuildIndex))
		switch p.Type {
		case 0:
			g.Fame = int(p.Value)
		case 1:
			g.Citizen = int(p.Value)
		case 2:
			g.Wins = int(p.Value)
		case 3: // Go only: the guild leader moved the guild to another kingdom
			if p.Value != 7 && p.Value != 8 {
				w.mu.Unlock()
				return true
			}
			g.Kingdom = int(p.Value)
		}
		saved := *g
		w.mu.Unlock()
		s.saveGuild(ctx, ch, saved)
		s.broadcast(&p)

	case wire.OpAddSub:
		var p wire.AddSub
		// The C++ only checked GuildIndex > MAX_GUILD, so a negative index
		// wrote outside g_pGuild.
		if !decode(ch, pkt, &p) || p.GuildIndex <= 0 || p.SubIndex < 0 || p.SubIndex > 2 {
			return true
		}
		w.mu.Lock()
		g := w.guildLocked(int(p.GuildIndex))
		if p.Status == 1 {
			g.SubGuild[p.SubIndex] = ""
		} else {
			g.SubGuild[p.SubIndex] = wire.GameString(p.Name[:])
		}
		saved := *g
		w.mu.Unlock()
		s.saveGuild(ctx, ch, saved)
		s.broadcast(&p)

	case wire.OpPanelGuildList:
		reply := wire.PanelGuildList{Header: wire.NewHeader(wire.OpPanelGuildList, pkt.Header.ClientID)}
		w.mu.Lock()
		for i := range reply.Guild { // C++ copied g_pGuild[0..39]
			if g := w.guilds[i]; g != nil {
				reply.Guild[i] = wire.GuildInfo{
					Name: wire.NewMSVCString(g.Name), Kingdom: uint8(g.Kingdom),
					Fame: int32(g.Fame), Citizen: int32(g.Citizen), Wins: int32(g.Wins),
				}
				for j, sub := range g.SubGuild {
					reply.Guild[i].SubGuild[j] = wire.NewMSVCString(sub)
				}
			} else {
				reply.Guild[i] = wire.GuildInfo{Name: wire.NewMSVCString("")}
				for j := range reply.Guild[i].SubGuild {
					reply.Guild[i].SubGuild[j] = wire.NewMSVCString("")
				}
			}
		}
		w.mu.Unlock()
		ch.send(&reply)

	case wire.OpReloadDonateList:
		slots, err := s.st.DonateStore(ctx)
		if err != nil {
			ch.log.Error("reload donate store", "err", err)
			return true
		}
		w.mu.Lock()
		w.donate = [donateStores][donateSlots]store.DonateSlot{}
		for _, d := range slots {
			w.donate[d.Store][d.Slot] = d
		}
		w.mu.Unlock()
		s.broadcastRaw(pkt.Raw)

	case wire.OpBroadcastSetAvaible:
		var p wire.StoreSetAvaible
		// The C++ indexed g_pStore[Value][Value2] unchecked.
		if !decode(ch, pkt, &p) || p.Store < 0 || p.Store >= donateStores || p.Slot < 0 || p.Slot >= donateSlots {
			return true
		}
		w.mu.Lock()
		w.donate[p.Store][p.Slot].Available = int(p.Avaible)
		w.mu.Unlock()
		if err := s.st.SetDonateAvailable(ctx, int(p.Store), int(p.Slot), int(p.Avaible)); err != nil {
			ch.log.Error("save donate availability", "err", err)
		}
		s.broadcastRaw(pkt.Raw)

	case wire.OpBuyStoreSaveList:
		var p wire.StoreSlot
		if !decode(ch, pkt, &p) || p.Store < 0 || p.Store >= donateStores || p.Slot < 0 || p.Slot >= donateSlots {
			return true
		}
		w.mu.Lock()
		d := &w.donate[p.Store][p.Slot]
		// -1 is unlimited. The C++ also decremented 0 to -1, turning a
		// sold-out item unlimited.
		if d.Available > 0 {
			d.Available--
		}
		avail := d.Available
		w.mu.Unlock()
		if err := s.st.SetDonateAvailable(ctx, int(p.Store), int(p.Slot), avail); err != nil {
			ch.log.Error("save donate availability", "err", err)
		}
		raw := append([]byte(nil), pkt.Raw...)
		le16(raw[4:], wire.OpBroadcastStoreBuy)
		s.broadcastRaw(raw)

	case wire.OpBroadcastChat, wire.OpBroadcast899:
		s.broadcastRaw(pkt.Raw)

	case wire.OpServerNotice:
		var p wire.Notice96
		if !decode(ch, pkt, &p) || p.Text[0] == 0 {
			return true
		}
		for i := range s.cfg.ChannelIPs {
			if c := s.channel(i); c != nil {
				out := wire.Notice96{Header: wire.NewHeader(wire.OpServerNotice, uint16(i)), Text: p.Text}
				c.send(&out)
			}
		}

	case wire.OpNotifyKefraDeath:
		var p wire.KefraDeath
		if !decode(ch, pkt, &p) {
			return true
		}
		w.mu.Lock()
		first := w.kefra.FirstChannel == maxServerGroup
		if first {
			w.kefra.FirstChannel = ch.idx
			if name := wire.GameString(p.Name[:]); name != "" {
				w.kefra.Killer = name
			}
		}
		k := w.kefra
		w.mu.Unlock()
		if first {
			if err := s.st.SetKefra(ctx, k); err != nil {
				ch.log.Error("save kefra", "err", err)
			}
			ch.log.Info("kefra killed first", "killer", k.Killer)
			s.broadcast(&wire.FirstKefra{Header: wire.NewHeader(wire.OpFirstKefraNotify, 0), Channel: int32(ch.idx)})
		}

	case wire.OpUpdateSapphire:
		var p wire.Parm1
		if !decode(ch, pkt, &p) {
			return true
		}
		v := s.sapphire.Load()
		if p.Parm1 == 1 {
			v <<= 1
		} else {
			v >>= 1
		}
		v = min(max(v, 1), 35)
		s.sapphire.Store(v)
		if err := s.st.SetSapphire(ctx, int(v)); err != nil {
			ch.log.Error("save sapphire", "err", err)
		}
		// SendDBSignalParm1 sent sizeof(MSG_STANDARDPARM3).
		s.broadcast(&wire.Parm3{Header: wire.NewHeader(wire.OpUpdateSapphire, 0), Parm1: p.Parm1})

	default:
		return false
	}
	return true
}

func (w *world) guildLocked(id int) *store.Guild {
	g := w.guilds[id]
	if g == nil {
		g = &store.Guild{ID: id}
		w.guilds[id] = g
	}
	return g
}

func (s *Server) saveGuild(ctx context.Context, ch *channel, g store.Guild) {
	if g.Name == "" {
		return // fame/subs for a guild never created; the C++ skipped these in guilds.txt too
	}
	if err := s.st.UpsertGuild(ctx, g); err != nil {
		ch.log.Error("save guild", "guild", g.ID, "err", err)
	}
}

func le16(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }

// resetKefra is the weekly Wednesday 19:00 respawn (ProcessSecTimer).
func (s *Server) resetKefra(ctx context.Context) {
	w := s.world
	w.mu.Lock()
	w.kefra = store.KefraState{FirstChannel: maxServerGroup}
	w.mu.Unlock()
	if err := s.st.SetKefra(ctx, store.KefraState{FirstChannel: maxServerGroup}); err != nil {
		s.log.Error("save kefra reset", "err", err)
	}
	s.broadcast(&wire.FirstKefra{Header: wire.NewHeader(wire.OpFirstKefraNotify, 0), Channel: maxServerGroup})
	s.broadcast(&wire.Signal{Header: wire.NewHeader(wire.OpRebornKefra, 0)})
	s.log.Info("kefra reborn on all channels")
}
