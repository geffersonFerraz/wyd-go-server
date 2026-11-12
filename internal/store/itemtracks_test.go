package store

import (
	"context"
	"testing"
	"time"
)

func TestItemTrackLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	sword := [3]ItemEffect{{Effect: 2, Value: 9}}

	pickup := ItemTrackEvent{TrackID: 77, Kind: TrackPickup, At: now, Channel: 1, ToAccount: "OUTRO", ToChar: "Outro",
		Item: 1103, Effects: sword, Mob: "Lobo", DropX: 2100, DropY: 2100, DroppedAt: now.Add(-11 * time.Minute),
		KillerAccount: "DONO", KillerChar: "Dono"}
	if err := s.AddItemTrackEvent(ctx, pickup); err != nil {
		t.Fatal(err)
	}
	if err := s.AddItemTrackEvent(ctx, pickup); err == nil {
		t.Fatal("the same track was created twice")
	}
	if err := s.SyncItemTracks(ctx, 1, "OUTRO", "Outro", []ItemTrackPlace{{TrackID: 77, CharSlot: 1, Type: 1, Slot: 4, Item: 1103, Effects: sword}}); err != nil {
		t.Fatal(err)
	}
	places, err := s.AccountItemTracks(ctx, "OUTRO", 1)
	if err != nil || len(places) != 1 || places[0].Slot != 4 || places[0].Effects != sword {
		t.Fatalf("places %+v err %v", places, err)
	}
	if places, _ := s.AccountItemTracks(ctx, "OUTRO", 2); len(places) != 0 {
		t.Fatal("another character's slot listed the item")
	}

	if err := s.AddItemTrackEvent(ctx, ItemTrackEvent{TrackID: 77, Kind: TrackTrade, At: now, Channel: 1,
		FromAccount: "OUTRO", FromChar: "Outro", ToAccount: "TERCEIRO", ToChar: "Terceiro", Item: 1103, Effects: sword}); err != nil {
		t.Fatal(err)
	}
	if places, _ := s.AccountItemTracks(ctx, "OUTRO", 1); len(places) != 0 {
		t.Fatal("the old holder still lists the item")
	}
	if err := s.SyncItemTracks(ctx, 1, "TERCEIRO", "Terceiro", []ItemTrackPlace{{TrackID: 77, CharSlot: -1, Type: 2, Slot: 0, Item: 1103, Effects: sword}}); err != nil {
		t.Fatal(err)
	}
	if places, _ := s.AccountItemTracks(ctx, "TERCEIRO", 3); len(places) != 1 || places[0].CharSlot != -1 {
		t.Fatalf("storage item not listed for every character: %+v", places)
	}
	// Dropped on the ground: no longer in the slots, still the holder's.
	if err := s.SyncItemTracks(ctx, 1, "TERCEIRO", "Terceiro", []ItemTrackPlace{{TrackID: 77, Type: -1}}); err != nil {
		t.Fatal(err)
	}
	if places, _ := s.AccountItemTracks(ctx, "TERCEIRO", 3); len(places) != 0 {
		t.Fatal("an item on the ground was listed")
	}
	if err := s.AddItemTrackEvent(ctx, ItemTrackEvent{TrackID: 77, Kind: TrackNPCSell, At: now, Channel: 1,
		FromAccount: "TERCEIRO", FromChar: "Terceiro", Price: 500, Item: 1103, Effects: sword}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddItemTrackEvent(ctx, ItemTrackEvent{TrackID: 999, Kind: TrackTrade, At: now, Item: 1}); err == nil {
		t.Fatal("event of an unknown track stored")
	}

	tracks, err := s.SearchItemTracks(ctx, "terceiro", 10)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("search: %+v %v", tracks, err)
	}
	tr := tracks[0]
	if tr.Status != "sold" || tr.HolderAccount != "" || tr.KillerChar != "Dono" || tr.Mob != "Lobo" || !tr.DroppedAt.Equal(pickup.DroppedAt) {
		t.Fatalf("track: %+v", tr)
	}
	if byKiller, _ := s.SearchItemTracks(ctx, "dono", 10); len(byKiller) != 1 {
		t.Fatal("not found by the killer")
	}
	events, err := s.ItemTrackEvents(ctx, 77)
	if err != nil || len(events) != 3 {
		t.Fatalf("events %+v %v", events, err)
	}
	if events[0].Kind != TrackPickup || events[1].Kind != TrackTrade || events[1].ToAccount != "TERCEIRO" || events[2].Kind != TrackNPCSell || events[2].Price != 500 {
		t.Fatalf("history: %+v", events)
	}
}

func TestItemTrackFromNPC(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	e := ItemTrackEvent{TrackID: 5, Kind: TrackNPCBuy, At: time.Now(), Channel: 2, ToAccount: "DONO", ToChar: "Dono",
		Item: 1103, Mob: "Ferreiro", DropX: 2100, DropY: 2100, DroppedAt: time.Now(), Price: 300}
	if err := s.AddItemTrackEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.TrackID, e.Kind, e.Price = 6, TrackNPCReward, 0
	if err := s.AddItemTrackEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.SearchItemTracks(ctx, "dono", 10)
	if err != nil || len(tracks) != 2 {
		t.Fatalf("%+v %v", tracks, err)
	}
	got := map[int64]ItemTrack{tracks[0].ID: tracks[0], tracks[1].ID: tracks[1]}
	if b := got[5]; b.Origin != "npc_buy" || b.Price != 300 || b.Mob != "Ferreiro" || b.KillerAccount != "" || b.HolderAccount != "DONO" {
		t.Fatalf("bought: %+v", b)
	}
	if r := got[6]; r.Origin != "npc_reward" || r.Price != 0 {
		t.Fatalf("reward: %+v", r)
	}
	if ev, _ := s.ItemTrackEvents(ctx, 5); len(ev) != 1 || ev[0].Kind != TrackNPCBuy {
		t.Fatalf("events: %+v", ev)
	}
}

func TestItemTrackFromEvent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	e := ItemTrackEvent{TrackID: 8, Kind: TrackEventReward, At: time.Now(), Channel: 1, ToAccount: "DONO", ToChar: "Dono",
		Item: 1740, Mob: "Roleta", DroppedAt: time.Now()}
	if err := s.AddItemTrackEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	tracks, err := s.SearchItemTracks(ctx, "8", 10)
	if err != nil || len(tracks) != 1 || tracks[0].Origin != "event_reward" || tracks[0].Mob != "Roleta" {
		t.Fatalf("%+v %v", tracks, err)
	}
}

func TestFindItemTracks(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.SaveItem(ctx, &Item{ID: 1103, Name: "Espada_Longa"}, "test"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Millisecond)
	add := func(e ItemTrackEvent) {
		t.Helper()
		e.Channel = 1
		if e.At.IsZero() {
			e.At = now
		}
		if err := s.AddItemTrackEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	// Track 1: ANA picks the sword up, trades it to BIA, who sells it to an NPC.
	add(ItemTrackEvent{TrackID: 1, Kind: TrackPickup, ToAccount: "ANA", ToChar: "Ana", Item: 1103, Mob: "Lobo", DroppedAt: now})
	add(ItemTrackEvent{TrackID: 1, Kind: TrackTrade, FromAccount: "ANA", FromChar: "Ana", ToAccount: "BIA", ToChar: "Bia", Item: 1103})
	add(ItemTrackEvent{TrackID: 1, Kind: TrackNPCSell, FromAccount: "BIA", FromChar: "Bia", Item: 1103, Price: 50})
	// Track 2: another sword, still with CAIO. Track 3: another item, with BIA.
	add(ItemTrackEvent{TrackID: 2, Kind: TrackNPCBuy, ToAccount: "CAIO", ToChar: "Caio", Item: 1103, Mob: "Ferreiro", DroppedAt: now})
	add(ItemTrackEvent{TrackID: 3, Kind: TrackEventReward, ToAccount: "BIA", ToChar: "Bia", Item: 1740, Mob: "Roleta", DroppedAt: now})

	ids := func(f ItemTrackFilter) map[int64]ItemTrack {
		t.Helper()
		f.Limit = 10
		list, err := s.FindItemTracks(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]ItemTrack{}
		for _, tr := range list {
			out[tr.ID] = tr
		}
		return out
	}
	// ANA no longer holds anything, but track 1 went through the account.
	if got := ids(ItemTrackFilter{Account: "ana"}); len(got) != 1 || len(got[1].Events) != 3 || got[1].Events[1].ToAccount != "BIA" {
		t.Fatalf("by past holder: %+v", got)
	}
	if got := ids(ItemTrackFilter{Account: "BIA"}); len(got) != 2 || got[3].HolderAccount != "BIA" || got[1].Status != "sold" {
		t.Fatalf("by account: %+v", got)
	}
	if got := ids(ItemTrackFilter{Item: "1103"}); len(got) != 2 || got[2].ItemName != "Espada_Longa" || got[2].CurrentItemName != "Espada_Longa" {
		t.Fatalf("by item number: %+v", got)
	}
	if got := ids(ItemTrackFilter{Item: "espada_l"}); len(got) != 2 {
		t.Fatalf("by item name: %+v", got)
	}
	if got := ids(ItemTrackFilter{Item: "espada%l"}); len(got) != 0 {
		t.Fatalf("a %% in the name worked as a wildcard: %+v", got)
	}
	if got := ids(ItemTrackFilter{Account: "bia", Item: "1740"}); len(got) != 1 || got[3].Mob != "Roleta" {
		t.Fatalf("by account and item: %+v", got)
	}
	if got := ids(ItemTrackFilter{ID: 2}); len(got) != 1 || len(got[2].Events) != 1 {
		t.Fatalf("by id: %+v", got)
	}
	if got := ids(ItemTrackFilter{}); len(got) != 3 {
		t.Fatalf("no filter: %+v", got)
	}
	if got := ids(ItemTrackFilter{Account: "ninguem"}); len(got) != 0 {
		t.Fatalf("unknown account: %+v", got)
	}
}

func TestItemTrackChanges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Millisecond)
	plus0 := [3]ItemEffect{{Effect: 43, Value: 0}}
	plus7 := [3]ItemEffect{{Effect: 43, Value: 7}}
	plus9 := [3]ItemEffect{{Effect: 43, Value: 9}}
	if err := s.AddItemTrackEvent(ctx, ItemTrackEvent{TrackID: 4, Kind: TrackPickup, At: now, Channel: 2, ToAccount: "ANA", ToChar: "Ana",
		Item: 1103, Effects: plus0, Mob: "Lobo", DroppedAt: now}); err != nil {
		t.Fatal(err)
	}
	place := ItemTrackPlace{TrackID: 4, CharSlot: 0, Type: 1, Slot: 3, Item: 1103}
	// The same item again: nothing new. Refined to +7: one 'changed' event.
	for _, eff := range [][3]ItemEffect{plus0, plus7, plus7} {
		place.Effects = eff
		if err := s.SyncItemTracks(ctx, 2, "ANA", "Ana", []ItemTrackPlace{place}); err != nil {
			t.Fatal(err)
		}
	}
	// Refined to +9 and traded before the next save: the change comes first, as ANA's.
	if err := s.AddItemTrackEvent(ctx, ItemTrackEvent{TrackID: 4, Kind: TrackTrade, At: now, Channel: 2, FromAccount: "ANA", FromChar: "Ana",
		ToAccount: "BIA", ToChar: "Bia", Item: 1103, Effects: plus9}); err != nil {
		t.Fatal(err)
	}
	ev, err := s.ItemTrackEvents(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range ev {
		kinds = append(kinds, e.Kind)
	}
	if len(ev) != 4 || ev[1].Kind != TrackChanged || ev[1].Effects != plus7 || ev[1].ToAccount != "ANA" || ev[1].Channel != 2 ||
		ev[2].Kind != TrackChanged || ev[2].Effects != plus9 || ev[2].FromAccount != "ANA" || ev[3].Kind != TrackTrade {
		t.Fatalf("events %v: %+v", kinds, ev)
	}
	tracks, _ := s.FindItemTracks(ctx, ItemTrackFilter{ID: 4, Limit: 1})
	if len(tracks) != 1 || tracks[0].Effects != plus0 || tracks[0].CurrentEffects != plus9 {
		t.Fatalf("track: %+v", tracks)
	}
}
