package game

import (
	"testing"

	"dbsrv/internal/wire"
)

// trackScene is dropScene with account names and the DBSRV link captured.
func trackScene(t *testing.T) (*Game, *User, *User, chan []byte) {
	t.Helper()
	g, a, b := dropScene(t)
	wire.PutCString(a.account.Username[:], "DONO")
	wire.PutCString(b.account.Username[:], "OUTRO")
	a.charSlot, b.charSlot = 0, 1
	g.db = &dbLink{g: g, out: make(chan []byte, 4096)}
	return g, a, b, g.db.out
}

// drainTracks reads the tracking packets sent so far.
func drainTracks(out chan []byte) (events []wire.ItemTrackEvent, places []wire.ItemTrackPlace) {
	for {
		select {
		case b := <-out:
			var h wire.Header
			_ = wire.Unmarshal(b, &h)
			switch h.PacketID {
			case wire.OpItemTrackEvent:
				var e wire.ItemTrackEvent
				_ = wire.Unmarshal(b, &e)
				events = append(events, e)
			case wire.OpItemTrackSync:
				var p wire.ItemTrackPlaces
				_ = wire.Unmarshal(b, &p)
				places = append(places, p.Places[:p.Count]...)
			}
		default:
			return events, places
		}
	}
}

// tracked gives a player a tracked item at an inventory slot.
func tracked(g *Game, u *User, slot int, it wire.Item, id int64) {
	g.mobs[u.id].info.Player.Inventory[slot] = it
	u.tracks = append(u.tracks, &trackRef{id: id, item: it, typ: wire.SlotInv, slot: slot, sent: unsent})
}

func refOf(u *User, id int64) *trackRef {
	for _, r := range u.tracks {
		if r.id == id {
			return r
		}
	}
	return nil
}

func TestMobDropTrackedForThePublicPicker(t *testing.T) {
	g, a, b, out := trackScene(t)
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Items: []DropItem{{Kills: 1, Index: 1103}}})
	g.handleMobDrop(a, "Teste", testOrigin(g))
	id := groundIDOf(g, 1103)
	if id == 0 {
		t.Fatal("the drop is not on the ground")
	}
	pickFrom(g, b, id)
	if !g.ground[id].open {
		t.Fatal("another player picked the killer's drop while reserved")
	}
	g.ground[id].ownerUntil = g.now() - 1
	pickFrom(g, b, id)
	slot := firstSlotOf(&g.mobs[2].info.Player, 1103)
	if g.ground[id].open || slot < 0 {
		t.Fatal("the public drop could not be picked")
	}
	events, _ := drainTracks(out)
	if len(events) != 1 {
		t.Fatalf("%d events, want the pickup", len(events))
	}
	e := events[0]
	if e.Kind != wire.TrackPickup || e.TrackID == 0 || wire.CString(e.ToAccount[:]) != "OUTRO" || wire.CString(e.ToChar[:]) != "Outro" ||
		wire.CString(e.KillerAccount[:]) != "DONO" || wire.CString(e.KillerChar[:]) != "Dono" || wire.CString(e.Mob[:]) != "Teste" || e.Item.Index != 1103 {
		t.Fatalf("pickup event: %+v", e)
	}
	if r := refOf(b, e.TrackID); r == nil || r.typ != wire.SlotInv || r.slot != slot {
		t.Fatalf("picker's ref: %+v", r)
	}
}

func TestStackableDropIsNotTracked(t *testing.T) {
	g, a, _, out := trackScene(t)
	writeDrops(t, g, &MobDrop{Name: "Teste", MaxDrops: 5, Items: []DropItem{{Kills: 1, Index: 4117}}})
	pl := &g.mobs[1].info.Player
	pl.Inventory[20] = wire.Item{Index: 4117, Effect: [3]wire.ItemEffect{{Index: EF_AMOUNT, Value: 2}}}
	g.handleMobDrop(a, "Teste", testOrigin(g))
	pickFrom(g, a, groundIDOf(g, 4117))
	if itemAmount(&pl.Inventory[20]) != 3 {
		t.Fatalf("the picked stack did not join the one in the inventory: %+v", pl.Inventory[20])
	}
	if events, _ := drainTracks(out); len(events) != 0 || len(a.tracks) != 0 {
		t.Fatalf("stackable item tracked: %+v", events)
	}
}

func TestTrackFollowsMoveAndTrade(t *testing.T) {
	g, a, b, out := trackScene(t)
	sword := wire.Item{Index: 1103, Effect: [3]wire.ItemEffect{{Index: 2, Value: 9}}}
	tracked(g, a, 0, sword, 42)

	g.mobs[1].target = wire.Position{X: uint16(g.data.Cities[0].CityMinX + 1), Y: uint16(g.data.Cities[0].CityMinY + 1)}
	moveItem(g, a, wire.SlotInv, 0, wire.SlotInv, 10)
	if r := refOf(a, 42); r == nil || r.slot != 10 {
		t.Fatalf("ref did not follow the move: %+v", r)
	}

	a.trade = offerOf(2, 0, 1, map[int]wire.Item{10: sword})
	b.trade = offerOf(1, 0, 1, nil)
	g.completeTrade(a, b)
	slot := firstSlotOf(&g.mobs[2].info.Player, 1103)
	if slot < 0 {
		t.Fatal("trade did not happen")
	}
	if len(a.tracks) != 0 {
		t.Fatal("the seller kept the track")
	}
	if r := refOf(b, 42); r == nil || r.typ != wire.SlotInv || r.slot != slot {
		t.Fatalf("buyer's ref: %+v", r)
	}
	events, places := drainTracks(out)
	if len(events) != 1 || events[0].Kind != wire.TrackTrade || wire.CString(events[0].FromAccount[:]) != "DONO" ||
		wire.CString(events[0].ToAccount[:]) != "OUTRO" || events[0].TrackID != 42 {
		t.Fatalf("trade events: %+v", events)
	}
	found := false
	for _, p := range places {
		if p.TrackID == 42 && p.CharSlot == 1 && p.Type == wire.SlotInv && int(p.Slot) == slot {
			found = true
		}
	}
	if !found {
		t.Fatalf("no place for the buyer after the trade's save: %+v", places)
	}
}

func TestTrackedItemDroppedAndPickedByAnother(t *testing.T) {
	g, a, b, out := trackScene(t)
	pos := g.mobs[1].target
	tracked(g, a, 4, wire.Item{Index: 1103}, 7)
	dropFrom(g, a, 4, int(pos.X)+1, int(pos.Y))
	id := groundIDOf(g, 1103)
	if id == 0 || g.ground[id].track == nil || len(a.tracks) != 0 {
		t.Fatalf("the track did not go to the ground: id %d", id)
	}
	g.ground[id].ownerUntil = g.now() - 1
	pickFrom(g, b, id)
	if refOf(b, 7) == nil {
		t.Fatal("the picker has no ref")
	}
	events, places := drainTracks(out)
	left := false
	for _, p := range places {
		left = left || (p.TrackID == 7 && p.Type == -1)
	}
	if !left {
		t.Fatalf("DBSRV was not told the item left the slots: %+v", places)
	}
	if len(events) != 1 || events[0].Kind != wire.TrackGround || wire.CString(events[0].FromChar[:]) != "Dono" || wire.CString(events[0].ToChar[:]) != "Outro" {
		t.Fatalf("events: %+v", events)
	}
}

func TestOwnerPicksBackWithoutEvent(t *testing.T) {
	g, a, _, out := trackScene(t)
	pos := g.mobs[1].target
	tracked(g, a, 4, wire.Item{Index: 1103}, 7)
	dropFrom(g, a, 4, int(pos.X)+1, int(pos.Y))
	pickFrom(g, a, groundIDOf(g, 1103))
	if refOf(a, 7) == nil {
		t.Fatal("the owner lost the track")
	}
	if events, _ := drainTracks(out); len(events) != 0 {
		t.Fatalf("events: %+v", events)
	}
}

func TestTrackedItemDestroyedOnTheGround(t *testing.T) {
	g, a, _, out := trackScene(t)
	pos := g.mobs[1].target
	tracked(g, a, 4, wire.Item{Index: 1103}, 7)
	dropFrom(g, a, 4, int(pos.X)+1, int(pos.Y))
	id := groundIDOf(g, 1103)
	g.ground[id].expireAt = g.now() - 1
	g.expirePlayerDrops()
	events, _ := drainTracks(out)
	if g.ground[id].open || len(events) != 1 || events[0].Kind != wire.TrackDestroyed || events[0].TrackID != 7 {
		t.Fatalf("events: %+v", events)
	}
}

func TestTrackFindsMovedItemAndReportsGone(t *testing.T) {
	g, a, _, out := trackScene(t)
	pl := &g.mobs[1].info.Player
	sword := wire.Item{Index: 1103, Effect: [3]wire.ItemEffect{{Index: 2, Value: 9}}}
	tracked(g, a, 0, sword, 1)
	tracked(g, a, 2, wire.Item{Index: 1104}, 2)
	// Moved some way the handlers do not follow: found by its copy.
	pl.Inventory[0] = wire.Item{}
	a.account.StorageItems[5] = sword
	// And the other one is used up.
	pl.Inventory[2] = wire.Item{}
	g.syncTracks(a)
	events, places := drainTracks(out)
	if r := refOf(a, 1); r == nil || r.typ != wire.SlotStorage || r.slot != 5 {
		t.Fatalf("moved item: %+v", r)
	}
	if len(places) != 1 || places[0].TrackID != 1 || places[0].CharSlot != -1 || places[0].Slot != 5 {
		t.Fatalf("places: %+v", places)
	}
	if refOf(a, 2) != nil || len(events) != 1 || events[0].Kind != wire.TrackGone || events[0].TrackID != 2 {
		t.Fatalf("gone: %+v", events)
	}
	// Nothing changed: nothing more is sent.
	g.syncTracks(a)
	if events, places := drainTracks(out); len(events)+len(places) != 0 {
		t.Fatalf("resent: %+v %+v", events, places)
	}
}

func TestRefinedItemKeepsItsTrack(t *testing.T) {
	g, a, _, out := trackScene(t)
	tracked(g, a, 0, wire.Item{Index: 1103}, 1)
	g.mobs[1].info.Player.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 3}
	g.syncTracks(a)
	events, places := drainTracks(out)
	if len(events) != 0 || len(places) != 1 || places[0].Item.Effect[0].Value != 3 {
		t.Fatalf("events %+v places %+v", events, places)
	}
}

func TestTrackSoldToNPCAndBoughtBack(t *testing.T) {
	g, a, _, out := trackScene(t)
	pl := &g.mobs[1].info.Player
	pl.Equip[13] = wire.Item{Index: 3914} // sells from anywhere
	tracked(g, a, 3, wire.Item{Index: 1103}, 9)
	g.requestSellShop(a, packet(&wire.SellShop{Header: wire.NewHeader(0x37A, 1), Type: wire.SlotInv, SellSlot: 3}))
	if pl.Inventory[3].Index != 0 {
		t.Fatal("not sold")
	}
	events, _ := drainTracks(out)
	if len(a.tracks) != 0 || len(events) != 1 || events[0].Kind != wire.TrackNPCSell || events[0].TrackID != 9 {
		t.Fatalf("sell: %+v", events)
	}
	pl.Gold = 1 << 30
	g.buyBack(a, 0)
	slot := firstSlotOf(pl, 1103)
	events, _ = drainTracks(out)
	if r := refOf(a, 9); slot < 0 || r == nil || r.slot != slot || len(events) != 1 || events[0].Kind != wire.TrackBuyback {
		t.Fatalf("buyback: slot %d ref %+v events %+v", slot, r, events)
	}
}

func TestItemTrackListOnLogin(t *testing.T) {
	g, a, _, _ := trackScene(t)
	p := wire.ItemTrackPlaces{Header: wire.NewHeader(wire.OpItemTrackList, 1), CharSlot: 0, Count: 2}
	wire.PutCString(p.Account[:], "DONO")
	p.Places[0] = wire.ItemTrackPlace{TrackID: 5, CharSlot: 0, Type: wire.SlotInv, Slot: 3, Item: wire.Item{Index: 1103}}
	p.Places[1] = wire.ItemTrackPlace{TrackID: 6, CharSlot: -1, Type: wire.SlotStorage, Slot: 1, Item: wire.Item{Index: 1104}}
	g.itemTrackList(a, packet(&p))
	if len(a.tracks) != 2 || refOf(a, 6).typ != wire.SlotStorage {
		t.Fatalf("tracks: %+v", a.tracks)
	}
	wire.PutCString(p.Account[:], "OUTRO")
	a.tracks = nil
	g.itemTrackList(a, packet(&p))
	if len(a.tracks) != 0 {
		t.Fatal("took the tracks of another account")
	}
}

func TestItemBoughtFromNPCIsTracked(t *testing.T) {
	g := worldGame(t)
	npc, slot := 0, 0
	for id := firstNPC; id < MaxMob && npc == 0; id++ {
		m := &g.mobs[id]
		if m.mode == 0 || isMerchant(&m.info.Player) == 0 || wire.CString(m.info.Player.Name[:]) == honorShopName {
			continue
		}
		for s := range 27 {
			it := m.info.Player.Inventory[s]
			if p := g.data.Item(it.Index).Price; trackable(&it) && p > 0 && p < 1000 {
				npc, slot = id, s
				break
			}
		}
	}
	if npc == 0 {
		t.Fatal("no merchant selling a non-stackable item")
	}
	nt := g.mobs[npc].target
	u := placePlayer(g, int(nt.X)+1, int(nt.Y), 5000)
	wire.PutCString(u.account.Username[:], "DONO")
	u.charSlot = 0
	g.db = &dbLink{g: g, out: make(chan []byte, 4096)}
	pl := &g.mobs[1].info.Player
	item := g.mobs[npc].info.Player.Inventory[slot]

	g.requestBuyShop(u, packet(&wire.BuyShop{MobID: int16(npc), SellSlot: int16(slot), InvSlot: 3}))
	if pl.Inventory[3].Index != item.Index {
		t.Fatal("not bought")
	}
	events, _ := drainTracks(g.db.out)
	if len(events) != 1 {
		t.Fatalf("events: %+v", events)
	}
	e := events[0]
	if e.Kind != wire.TrackNPCBuy || wire.GameString(e.Mob[:]) != g.charName(npc) ||
		e.Price != 5000-pl.Gold || wire.CString(e.ToAccount[:]) != "DONO" || wire.CString(e.KillerAccount[:]) != "" {
		t.Fatalf("buy event: %+v", e)
	}
	if r := refOf(u, e.TrackID); r == nil || r.slot != 3 {
		t.Fatalf("ref: %+v", r)
	}

	// Selling it and buying it back keeps the same track, no new one.
	g.requestSellShop(u, packet(&wire.SellShop{NpcID: int16(npc), Type: wire.SlotInv, SellSlot: 3}))
	g.requestBuyShop(u, packet(&wire.BuyShop{MobID: 0, SellSlot: 0, InvSlot: 0}))
	events, _ = drainTracks(g.db.out)
	if len(events) != 2 || events[0].Kind != wire.TrackNPCSell || events[1].Kind != wire.TrackBuyback || events[1].TrackID != e.TrackID {
		t.Fatalf("sell and buyback: %+v", events)
	}
}

func TestNPCRewardTracksOnlyNewItems(t *testing.T) {
	g, a, _, out := trackScene(t)
	pl := &g.mobs[1].info.Player
	npc := firstNPC
	for g.mobs[npc].mode == 0 {
		npc++
	}
	pl.Inventory = [64]wire.Item{}
	pl.Inventory[0] = wire.Item{Index: 1103} // refined by the NPC
	pl.Inventory[2] = wire.Item{Index: 1104} // moved by the NPC
	pl.Inventory[4] = wire.Item{Index: 1105} // taken by the NPC
	pl.Inventory[63].Effect[0].Index = 75    // PK points item
	done := g.npcWatch(a, npc, wire.TrackNPCReward)
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 1}
	pl.Inventory[2], pl.Inventory[6] = wire.Item{}, wire.Item{Index: 1104}
	pl.Inventory[4] = wire.Item{}
	g.putItem(a, &wire.Item{Index: 1106})                              // the reward
	g.putItem(a, &wire.Item{Index: 412, Effect: [3]wire.ItemEffect{}}) // stacks: not tracked
	done()
	events, _ := drainTracks(out)
	if len(events) != 1 || events[0].Kind != wire.TrackNPCReward || events[0].Item.Index != 1106 ||
		wire.GameString(events[0].Mob[:]) != g.charName(npc) {
		t.Fatalf("events: %+v", events)
	}
	if len(a.tracks) != 1 || a.tracks[0].item.Index != 1106 {
		t.Fatalf("tracks: %+v", a.tracks)
	}
}

func TestNPCRewardOnTheGroundIsTrackedWhenPicked(t *testing.T) {
	g, a, _, out := trackScene(t)
	pl := &g.mobs[1].info.Player
	npc := firstNPC
	for g.mobs[npc].mode == 0 {
		npc++
	}
	for i := range invSlots {
		pl.Inventory[i] = wire.Item{Index: 1103}
	}
	done := g.npcWatch(a, npc, wire.TrackNPCReward)
	g.putItem(a, &wire.Item{Index: 1106})
	done()
	id := groundIDOf(g, 1106)
	if id == 0 || g.ground[id].origin == nil || g.ground[id].origin.kind != wire.TrackNPCReward {
		t.Fatalf("reward not on the ground with its origin: %d", id)
	}
	if events, _ := drainTracks(out); len(events) != 0 {
		t.Fatalf("tracked before being picked: %+v", events)
	}
	pl.Inventory = [64]wire.Item{}
	pickFrom(g, a, id)
	if events, _ := drainTracks(out); len(events) != 1 || events[0].Kind != wire.TrackNPCReward {
		t.Fatalf("pickup: %+v", events)
	}
}

func TestEventPrizeIsTracked(t *testing.T) {
	g, a, _, out := trackScene(t)
	pl := &g.mobs[1].info.Player
	pl.Inventory = [64]wire.Item{}
	g.eventItem(a, &wire.Item{Index: 1106}, "Roleta")
	g.eventItem(a, &wire.Item{Index: 412}, "Roleta") // stacks: not tracked
	events, _ := drainTracks(out)
	if len(events) != 1 || events[0].Kind != wire.TrackEventReward || wire.CString(events[0].Mob[:]) != "Roleta" ||
		events[0].Item.Index != 1106 || wire.CString(events[0].ToAccount[:]) != "DONO" {
		t.Fatalf("events: %+v", events)
	}
	if r := refOf(a, events[0].TrackID); r == nil || pl.Inventory[r.slot].Index != 1106 {
		t.Fatalf("ref: %+v", r)
	}

	// No room: the prize falls on the ground and is tracked when picked up.
	for i := range invSlots {
		pl.Inventory[i] = wire.Item{Index: 1103}
	}
	g.eventItem(a, &wire.Item{Index: 1107}, "Missão")
	id := groundIDOf(g, 1107)
	if id == 0 || g.ground[id].origin == nil || g.ground[id].origin.mob != "Missão" {
		t.Fatalf("prize on the ground: %d", id)
	}
	pl.Inventory = [64]wire.Item{}
	pickFrom(g, a, id)
	var picked []wire.ItemTrackEvent
	events, _ = drainTracks(out)
	for _, e := range events {
		if e.Kind != wire.TrackGone { // the 1106 the test overwrote
			picked = append(picked, e)
		}
	}
	if len(picked) != 1 || picked[0].Kind != wire.TrackEventReward || wire.GameString(picked[0].Mob[:]) != "Missão" {
		t.Fatalf("pickup: %+v", events)
	}
}

func TestAlmaPrizeTrackedWithTheMonster(t *testing.T) {
	g, u, pl := moveGame(t)
	wire.PutCString(u.account.Username[:], "DONO")
	u.charSlot = 0
	g.db = &dbLink{g: g, out: make(chan []byte, 4096)}
	for range almaKills {
		g.monsterPrize(genAlmaUni, 1)
	}
	if firstSlotOf(pl, 1740) < 0 {
		t.Fatal("no soul")
	}
	events, _ := drainTracks(g.db.out)
	want := wire.DecodeGame(wire.CString(g.data.Geners[genAlmaUni].Leader.Name[:]))
	if len(events) != 1 || events[0].Kind != wire.TrackEventReward || events[0].Item.Index != 1740 || want == "" ||
		wire.GameString(events[0].Mob[:]) != want {
		t.Fatalf("want %q, events: %+v", want, events)
	}
}
