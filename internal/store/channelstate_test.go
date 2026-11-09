package store

import (
	"context"
	"testing"
)

func TestChannelStateAndMacBans(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if st, err := s.LoadChannelState(ctx, 9); err != nil || st != nil {
		t.Fatalf("empty: %+v %v", st, err)
	}
	st := &ChannelState{WeekMode: 5, KefraDead: true}
	st.Cities[1] = CityState{Impost: 1 << 40, Tax: 7, Bids: []CityBid{{Guild: 3, Value: 900}}}
	if err := s.SaveChannelState(ctx, 9, st); err != nil {
		t.Fatal(err)
	}
	st.WeekMode = 2
	if err := s.SaveChannelState(ctx, 9, st); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadChannelState(ctx, 9)
	if err != nil || got.WeekMode != 2 || !got.KefraDead || got.Cities[1].Impost != 1<<40 || got.Cities[1].Bids[0].Value != 900 {
		t.Fatalf("got %+v err %v", got, err)
	}
	mac := [8]byte{1, 2, 3, 4, 5, 6}
	if err := s.SetMacBan(ctx, mac, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMacBan(ctx, mac, true); err != nil {
		t.Fatal("banning twice:", err)
	}
	list, err := s.MacBans(ctx)
	if err != nil || len(list) != 1 || list[0] != mac {
		t.Fatalf("bans %v err %v", list, err)
	}
	if err := s.SetMacBan(ctx, mac, false); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.MacBans(ctx); len(list) != 0 {
		t.Fatalf("still banned: %v", list)
	}
}

func TestOpenStores(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.TouchOpenStore(ctx, 2, "Heroi", "Loja do Heroi", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchOpenStore(ctx, 2, "Heroi", "Nova", 4); err != nil {
		t.Fatal(err)
	}
	list, err := s.OpenStores(ctx)
	if err != nil || len(list) != 1 || list[0].Name != "Nova" || list[0].Items != 4 {
		t.Fatalf("stores %+v err %v", list, err)
	}
}

func TestSeedRateDropsOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.SaveDrop(ctx, "", &Drop{Name: "Lobo", MaxDrops: 2, Items: []DropItem{{Kills: 3, Item: 400}}}, "t"); err != nil {
		t.Fatal(err)
	}
	list := []Drop{{Name: "Lobo", RateGold: 7, Rate: []RateDrop{{Slot: 1, Item: 412}}}, {Name: "Urso", Rate: []RateDrop{{Slot: 2, Item: 413}}}}
	n, err := s.SeedRateDrops(ctx, list, "t")
	if err != nil || n != 2 {
		t.Fatalf("lines %d err %v", n, err)
	}
	d, _ := s.GetDrop(ctx, "Lobo")
	if len(d.Items) != 1 || d.MaxDrops != 2 || d.RateGold != 7 || len(d.Rate) != 1 {
		t.Fatalf("existing monster: %+v", d)
	}
	if err := s.SaveDrop(ctx, "Lobo", &Drop{Name: "Lobo"}, "t"); err != nil { // the panel clears it
		t.Fatal(err)
	}
	if n, _ := s.SeedRateDrops(ctx, list, "t"); n != 0 {
		t.Fatalf("seeded twice: %d", n)
	}
	gd, err := s.LoadGameData(ctx)
	if err != nil || len(gd.DropSlots) != 64 || len(gd.BlockedDrops) == 0 {
		t.Fatalf("slots %d blocked %d err %v", len(gd.DropSlots), len(gd.BlockedDrops), err)
	}
}
