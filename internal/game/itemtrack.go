package game

import (
	"math"
	"math/rand/v2"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Item tracking. A monster's drop falls on the ground with its origin (the
// monster, where, when, and who killed it). The first player to pick it up
// gives it a track id, and from then on every time it changes account
// (trade, store, ground) DBSRV appends an event to its history. Only items
// that do not stack are tracked; a drop nobody picks up vanishes untracked.
// Items bought from an NPC or given by one (quests, events), and the
// prizes of the events, are tracked as they arrive, with the NPC or the
// event as their origin (see npcWatch and eventItem).
//
// The id cannot live inside the item (wire.Item is the client's 14 bytes),
// so each player keeps a list of trackRefs: the id and the slot its item is
// in. The handlers that move items between accounts carry the refs along;
// RequestMoveItem moves them with the items; an item changed in place
// (refined, timed) keeps its ref as long as the index is the same, and one
// moved some other way is found again by reconcileTracks by its exact
// index and effects. A ref whose item is no longer anywhere in the
// account is reported gone.

// trackRef is one tracked item a player holds.
type trackRef struct {
	id        int64
	item      wire.Item // as last seen
	typ, slot int       // where it is; typ -1 = not placed yet
	sent      trackPlace
}

// trackPlace is a place as told to DBSRV.
type trackPlace struct {
	charSlot, typ, slot int
	item                wire.Item
}

var unsent = trackPlace{typ: -2}

// dropOrigin is where a monster's drop came from.
type dropOrigin struct {
	kind                      int32  // TrackPickup (a monster's drop), TrackNPCBuy, TrackNPCReward, TrackEventReward
	mob                       string // the monster, the NPC or the event (UTF-8)
	x, y                      int
	at                        time.Time
	killerAccount, killerChar string // a monster's drop only
	price                     int64  // gold paid to the NPC
}

// trackParty is one side of a transfer.
type trackParty struct{ account, char string }

func (g *Game) partyOf(u *User) trackParty {
	return trackParty{account: wire.CString(u.account.Username[:]), char: g.charName(u.id)}
}

// trackable reports the items that get a track: those that do not stack.
func trackable(it *wire.Item) bool {
	return it.Index != 0 && int(it.Index) < MaxItemList && maxAmount[it.Index] == 0
}

func newTrackID() int64 { return rand.Int64N(math.MaxInt64-1) + 1 }

// trackItem is the item at a place of the playing character or its storage.
func (g *Game) trackItem(u *User, typ, slot int) *wire.Item {
	pl := &g.mobs[u.id].info.Player
	switch typ {
	case wire.SlotEquip:
		if slot > 0 && slot < len(pl.Equip) {
			return &pl.Equip[slot]
		}
	case wire.SlotInv:
		if slot >= 0 && slot < len(pl.Inventory) {
			return &pl.Inventory[slot]
		}
	case wire.SlotStorage:
		if slot >= 0 && slot < len(u.account.StorageItems) {
			return &u.account.StorageItems[slot]
		}
	}
	return nil
}

// reconcileTracks puts every ref back on the slot that holds its item.
func (g *Game) reconcileTracks(u *User) {
	if len(u.tracks) == 0 || u.status != userPlay {
		return
	}
	type place struct{ typ, slot int }
	claimed := map[place]bool{}
	var lost []*trackRef
	for _, r := range u.tracks {
		if it := g.trackItem(u, r.typ, r.slot); it != nil && it.Index == r.item.Index && !claimed[place{r.typ, r.slot}] {
			claimed[place{r.typ, r.slot}] = true
			r.item = *it
			continue
		}
		lost = append(lost, r)
	}
	if len(lost) == 0 {
		return
	}
	// The same item, unchanged, elsewhere in the account.
	find := func(r *trackRef) bool {
		for _, typ := range []int{wire.SlotInv, wire.SlotEquip, wire.SlotStorage} {
			for slot := 0; ; slot++ {
				it := g.trackItem(u, typ, slot)
				if it == nil {
					if typ == wire.SlotEquip && slot == 0 {
						continue
					}
					break
				}
				if *it != r.item || claimed[place{typ, slot}] {
					continue
				}
				claimed[place{typ, slot}] = true
				r.typ, r.slot, r.item = typ, slot, *it
				return true
			}
		}
		return false
	}
	for _, r := range lost {
		if find(r) {
			continue
		}
		g.log.Info("tracked item left the account", "slot", u.id, "track", r.id, "item", r.item.Index, "effects", r.item.Effect)
		g.trackEvent(wire.TrackGone, r.id, r.item, g.partyOf(u), trackParty{}, 0)
		g.dropRef(u, r)
	}
}

func (g *Game) dropRef(u *User, r *trackRef) {
	for i, x := range u.tracks {
		if x == r {
			u.tracks = append(u.tracks[:i], u.tracks[i+1:]...)
			return
		}
	}
}

// takeTrack removes and returns the ref of the item at (typ, slot), nil when
// that item is not tracked. Call it before the item leaves the slot.
func (g *Game) takeTrack(u *User, typ, slot int) *trackRef {
	g.reconcileTracks(u)
	for _, r := range u.tracks {
		if r.typ == typ && r.slot == slot {
			g.dropRef(u, r)
			return r
		}
	}
	return nil
}

// giveTrack hands a ref to a player, its item at (typ, slot) or, with typ
// -1, wherever reconcileTracks finds it.
func (g *Game) giveTrack(u *User, r *trackRef, typ, slot int) {
	if r == nil {
		return
	}
	r.typ, r.slot = typ, slot
	if it := g.trackItem(u, typ, slot); it != nil {
		r.item = *it
	}
	r.sent = unsent
	u.tracks = append(u.tracks, r)
}

// moveTracks follows a swap of two slots (RequestMoveItem).
func (u *User) moveTracks(srcType, srcSlot, dstType, dstSlot int) {
	for _, r := range u.tracks {
		switch {
		case r.typ == srcType && r.slot == srcSlot:
			r.typ, r.slot = dstType, dstSlot
		case r.typ == dstType && r.slot == dstSlot:
			r.typ, r.slot = srcType, srcSlot
		}
	}
}

// transferTracks moves refs from one player to another and records it.
func (g *Game) transferTracks(kind int32, refs []*trackRef, from, to *User, price int64) {
	for _, r := range refs {
		g.giveTrack(to, r, -1, -1)
		g.trackEvent(kind, r.id, r.item, g.partyOf(from), g.partyOf(to), price)
	}
	g.reconcileTracks(to)
}

// trackEvent sends one event to DBSRV.
func (g *Game) trackEvent(kind int32, id int64, it wire.Item, from, to trackParty, price int64) {
	p := wire.ItemTrackEvent{Header: wire.NewHeader(wire.OpItemTrackEvent, wire.ClientIDItemTrack),
		Kind: kind, TrackID: id, At: time.Now().UnixMilli(), Price: price, Channel: int32(g.cfg.Channel), Item: it}
	wire.PutCString(p.FromAccount[:], from.account)
	wire.PutGameString(p.FromChar[:], from.char)
	wire.PutCString(p.ToAccount[:], to.account)
	wire.PutGameString(p.ToChar[:], to.char)
	g.sendDB(&p)
}

// createTrack gives a monster's drop its id as a player picks it up.
func (g *Game) createTrack(u *User, o *dropOrigin, it wire.Item) *trackRef {
	kind := o.kind
	if kind == 0 {
		kind = wire.TrackPickup
	}
	r := &trackRef{id: newTrackID(), item: it, typ: -1, slot: -1, sent: unsent}
	to := g.partyOf(u)
	p := wire.ItemTrackEvent{Header: wire.NewHeader(wire.OpItemTrackEvent, wire.ClientIDItemTrack),
		Kind: kind, TrackID: r.id, At: time.Now().UnixMilli(), DroppedAt: o.at.UnixMilli(), Price: o.price,
		Channel: int32(g.cfg.Channel), DropX: int16(o.x), DropY: int16(o.y), Item: it}
	wire.PutCString(p.ToAccount[:], to.account)
	wire.PutGameString(p.ToChar[:], to.char)
	wire.PutGameString(p.Mob[:], o.mob)
	wire.PutCString(p.KillerAccount[:], o.killerAccount)
	wire.PutGameString(p.KillerChar[:], o.killerChar)
	g.sendDB(&p)
	g.log.Info("tracked item created", "slot", u.id, "track", r.id, "kind", kind, "item", it.Index, "effects", it.Effect,
		"origin", o.mob, "killer", o.killerChar)
	return r
}

// syncTracks tells DBSRV where the tracked items moved since the last time
// (with every save).
func (g *Game) syncTracks(u *User) {
	if u.charSlot < 0 || u.charSlot > 3 {
		return
	}
	g.reconcileTracks(u)
	p := wire.ItemTrackPlaces{Header: wire.NewHeader(wire.OpItemTrackSync, wire.ClientIDItemTrack), CharSlot: int32(u.charSlot)}
	wire.PutCString(p.Account[:], wire.CString(u.account.Username[:]))
	wire.PutGameString(p.Char[:], g.charName(u.id))
	flush := func() {
		if p.Count > 0 {
			q := p
			g.sendDB(&q)
			p.Count, p.Places = 0, [32]wire.ItemTrackPlace{}
		}
	}
	add := func(r *trackRef, pl trackPlace) {
		p.Places[p.Count] = wire.ItemTrackPlace{TrackID: r.id, CharSlot: int16(pl.charSlot), Type: int16(pl.typ), Slot: int16(pl.slot), Item: pl.item}
		p.Count++
		if int(p.Count) == len(p.Places) {
			flush()
		}
	}
	for _, r := range u.tracks {
		pl := trackPlace{charSlot: u.charSlot, typ: r.typ, slot: r.slot, item: r.item}
		if r.typ == wire.SlotStorage {
			pl.charSlot = -1
		}
		if pl == r.sent {
			continue
		}
		r.sent = pl
		add(r, pl)
	}
	flush()
}

// leaveTrack tells DBSRV a tracked item left its holder's slots without
// changing hands yet (it was dropped on the ground).
func (g *Game) leaveTrack(u *User, r *trackRef) {
	p := wire.ItemTrackPlaces{Header: wire.NewHeader(wire.OpItemTrackSync, wire.ClientIDItemTrack), CharSlot: int32(u.charSlot), Count: 1}
	wire.PutCString(p.Account[:], wire.CString(u.account.Username[:]))
	wire.PutGameString(p.Char[:], g.charName(u.id))
	p.Places[0] = wire.ItemTrackPlace{TrackID: r.id, CharSlot: -1, Type: -1, Slot: -1, Item: r.item}
	g.sendDB(&p)
	r.sent = unsent
}

// itemTrackList is 0xC22: the tracked items of the character that just
// entered the world.
func (g *Game) itemTrackList(u *User, pkt protocol.Packet) {
	var p wire.ItemTrackPlaces
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if wire.CString(p.Account[:]) != wire.CString(u.account.Username[:]) || int(p.CharSlot) != u.charSlot {
		g.log.Warn("item tracks for another character", "slot", u.id, "account", wire.CString(p.Account[:]), "char_slot", p.CharSlot)
		return
	}
	n := min(max(int(p.Count), 0), len(p.Places))
	for _, pl := range p.Places[:n] {
		if pl.TrackID == 0 || u.hasTrack(pl.TrackID) {
			continue
		}
		r := &trackRef{id: pl.TrackID, item: pl.Item, typ: int(pl.Type), slot: int(pl.Slot)}
		r.sent = trackPlace{charSlot: int(pl.CharSlot), typ: r.typ, slot: r.slot, item: r.item}
		u.tracks = append(u.tracks, r)
	}
}

func (u *User) hasTrack(id int64) bool {
	for _, r := range u.tracks {
		if r.id == id {
			return true
		}
	}
	return false
}

// npcGift is the NPC or event giving items to a player right now (watchGifts).
type npcGift struct {
	u      *User
	origin *dropOrigin
}

type slotPlace struct{ typ, slot int }

// trackSlots is every item of the character and its storage by place.
func (g *Game) trackSlots(u *User) map[slotPlace]wire.Item {
	out := map[slotPlace]wire.Item{}
	for _, typ := range []int{wire.SlotEquip, wire.SlotInv, wire.SlotStorage} {
		for slot := 0; ; slot++ {
			it := g.trackItem(u, typ, slot)
			if it == nil {
				if typ == wire.SlotEquip && slot == 0 {
					continue
				}
				break
			}
			if it.Index != 0 {
				out[slotPlace{typ, slot}] = *it
			}
		}
	}
	return out
}

// npcWatch starts an NPC's action on a player (a purchase or a quest/event
// reward, by kind); the function it returns ends it, tracking every new
// non-stackable item the NPC gave. An item is new when its place held
// nothing of that index before and it is not one that just moved; items
// the NPC dropped next to a full inventory carry the origin to the ground
// (see dropNearPlayer).
func (g *Game) npcWatch(u *User, npc int, kind int32) func() {
	if u.status != userPlay || npc <= 0 || npc >= MaxMob {
		return func() {}
	}
	t := g.mobs[npc].target
	return g.watchGifts(u, &dropOrigin{kind: kind, mob: g.charName(npc), x: int(t.X), y: int(t.Y), at: time.Now()})
}

// eventItem is putItem for an event's prize: tracked (when it does not
// stack) with the event as its origin.
func (g *Game) eventItem(u *User, it *wire.Item, event string) bool {
	if u == nil {
		return false
	}
	defer g.eventWatch(u, event)()
	return g.putItem(u, it)
}

// eventWatch is npcWatch for an event's prizes.
func (g *Game) eventWatch(u *User, event string) func() {
	if u == nil || u.status != userPlay {
		return func() {}
	}
	t := g.mobs[u.id].target
	return g.watchGifts(u, &dropOrigin{kind: wire.TrackEventReward, mob: event, x: int(t.X), y: int(t.Y), at: time.Now()})
}

// watchGifts is npcWatch for any origin.
func (g *Game) watchGifts(u *User, o *dropOrigin) func() {
	if u.status != userPlay {
		return func() {}
	}
	kind := o.kind
	g.reconcileTracks(u)
	before, charSlot := g.trackSlots(u), u.charSlot
	pl := &g.mobs[u.id].info.Player
	gold := pl.Gold
	prev := g.npcGift
	g.npcGift = &npcGift{u: u, origin: o}
	return func() {
		g.npcGift = prev
		if u.status != userPlay || u.charSlot != charSlot {
			return // the character changed (an Arch was created)
		}
		if spent := gold - pl.Gold; kind == wire.TrackNPCBuy && spent > 0 {
			o.price = spent
		}
		g.reconcileTracks(u)
		after := g.trackSlots(u)
		claimed := map[slotPlace]bool{}
		for _, r := range u.tracks {
			claimed[slotPlace{r.typ, r.slot}] = true
		}
		moved := map[wire.Item]int{} // items that left their place: maybe moved
		for p, it := range before {
			if after[p] != it {
				moved[it]++
			}
		}
		for p, it := range after {
			if claimed[p] || !trackable(&it) || before[p].Index == it.Index {
				continue // tracked already, stacks, or changed in place
			}
			if moved[it] > 0 {
				moved[it]--
				continue
			}
			g.giveTrack(u, g.createTrack(u, o, it), p.typ, p.slot)
		}
	}
}
