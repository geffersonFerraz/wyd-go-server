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
	if err := s.SyncItemTracks(ctx, "OUTRO", "Outro", []ItemTrackPlace{{TrackID: 77, CharSlot: 1, Type: 1, Slot: 4, Item: 1103, Effects: sword}}); err != nil {
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
	if err := s.SyncItemTracks(ctx, "TERCEIRO", "Terceiro", []ItemTrackPlace{{TrackID: 77, CharSlot: -1, Type: 2, Slot: 0, Item: 1103, Effects: sword}}); err != nil {
		t.Fatal(err)
	}
	if places, _ := s.AccountItemTracks(ctx, "TERCEIRO", 3); len(places) != 1 || places[0].CharSlot != -1 {
		t.Fatalf("storage item not listed for every character: %+v", places)
	}
	// Dropped on the ground: no longer in the slots, still the holder's.
	if err := s.SyncItemTracks(ctx, "TERCEIRO", "Terceiro", []ItemTrackPlace{{TrackID: 77, Type: -1}}); err != nil {
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
