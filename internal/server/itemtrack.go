package server

import (
	"context"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Item tracking: TMSRV reports the events of the tracked items (0xC20) and
// where they are after each save (0xC21); on login DBSRV hands a character
// its tracked items back (0xC22). See store/itemtracks.go.

var trackKinds = map[int32]string{
	wire.TrackPickup:      store.TrackPickup,
	wire.TrackNPCBuy:      store.TrackNPCBuy,
	wire.TrackNPCReward:   store.TrackNPCReward,
	wire.TrackEventReward: store.TrackEventReward,
	wire.TrackTrade:       store.TrackTrade,
	wire.TrackStore:       store.TrackStore,
	wire.TrackGround:      store.TrackGround,
	wire.TrackNPCSell:     store.TrackNPCSell,
	wire.TrackBuyback:     store.TrackBuyback,
	wire.TrackDestroyed:   store.TrackDestroyed,
	wire.TrackGone:        store.TrackGone,
}

func storeEffects(it wire.Item) [3]store.ItemEffect {
	var out [3]store.ItemEffect
	for i, e := range it.Effect {
		out[i] = store.ItemEffect{Effect: int(e.Index), Value: int(e.Value)}
	}
	return out
}

func unixMs(ms int64) time.Time {
	if ms <= 0 {
		return time.Now()
	}
	return time.UnixMilli(ms)
}

// handleItemTrackEvent (Go only, 0xC20) appends to an item's history.
func (s *Server) handleItemTrackEvent(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.ItemTrackEvent
	if !decode(ch, pkt, &p) {
		return
	}
	kind, ok := trackKinds[p.Kind]
	if !ok || p.TrackID == 0 {
		ch.log.Warn("item track event ignored", "kind", p.Kind, "track", p.TrackID)
		return
	}
	e := store.ItemTrackEvent{
		TrackID: p.TrackID, Kind: kind, At: unixMs(p.At), Channel: int(p.Channel), Price: p.Price,
		FromAccount: strings.ToUpper(wire.CString(p.FromAccount[:])), FromChar: wire.GameString(p.FromChar[:]),
		ToAccount: strings.ToUpper(wire.CString(p.ToAccount[:])), ToChar: wire.GameString(p.ToChar[:]),
		Item: int(p.Item.Index), Effects: storeEffects(p.Item),
		Mob: wire.GameString(p.Mob[:]), DropX: int(p.DropX), DropY: int(p.DropY), DroppedAt: unixMs(p.DroppedAt),
		KillerAccount: strings.ToUpper(wire.CString(p.KillerAccount[:])), KillerChar: wire.GameString(p.KillerChar[:]),
	}
	if err := s.st.AddItemTrackEvent(ctx, e); err != nil {
		ch.log.Error("item track event NOT stored", "kind", kind, "track", e.TrackID, "item", e.Item,
			"from", e.FromAccount, "to", e.ToAccount, "err", err)
	}
}

// handleItemTrackSync (Go only, 0xC21) records where tracked items are.
func (s *Server) handleItemTrackSync(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.ItemTrackPlaces
	if !decode(ch, pkt, &p) {
		return
	}
	account := strings.ToUpper(wire.CString(p.Account[:]))
	n := min(max(int(p.Count), 0), len(p.Places))
	if account == "" || n == 0 {
		return
	}
	places := make([]store.ItemTrackPlace, 0, n)
	for _, pl := range p.Places[:n] {
		places = append(places, store.ItemTrackPlace{TrackID: pl.TrackID, CharSlot: int(pl.CharSlot), Type: int(pl.Type),
			Slot: int(pl.Slot), Item: int(pl.Item.Index), Effects: storeEffects(pl.Item)})
	}
	if err := s.st.SyncItemTracks(ctx, account, wire.GameString(p.Char[:]), places); err != nil {
		ch.log.Error("item track places NOT stored", "account", account, "count", n, "err", err)
	}
}

// sendItemTracks sends 0xC22 with the character's tracked items (and its
// storage's), after 0x417. Nothing is sent when it has none.
func (ch *channel) sendItemTracks(client uint16, sess *session) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	places, err := ch.srv.st.AccountItemTracks(ctx, sess.username, sess.slot)
	if err != nil {
		ch.log.Error("item tracks NOT loaded", "account", sess.username, "slot", sess.slot, "err", err)
		return
	}
	for len(places) > 0 {
		p := wire.ItemTrackPlaces{Header: wire.NewHeader(wire.OpItemTrackList, client), CharSlot: int32(sess.slot)}
		wire.PutCString(p.Account[:], sess.username)
		n := copy(p.Places[:], toWirePlaces(places))
		p.Count = int32(n)
		ch.send(&p)
		places = places[n:]
	}
}

func toWirePlaces(places []store.ItemTrackPlace) []wire.ItemTrackPlace {
	out := make([]wire.ItemTrackPlace, 0, min(len(places), 32))
	for _, pl := range places[:min(len(places), 32)] {
		it := wire.Item{Index: uint16(pl.Item)}
		for i, e := range pl.Effects {
			it.Effect[i] = wire.ItemEffect{Index: uint8(e.Effect), Value: uint16(e.Value)}
		}
		out = append(out, wire.ItemTrackPlace{TrackID: pl.TrackID, CharSlot: int16(pl.CharSlot), Type: int16(pl.Type),
			Slot: int16(pl.Slot), Item: it})
	}
	return out
}
