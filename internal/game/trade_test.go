package game

import (
	"log/slog"
	"testing"

	"dbsrv/internal/wire"
)

func tradeGame(t *testing.T) (*Game, *User, *User) {
	t.Helper()
	d := &Data{Items: make([]ItemData, MaxItemList), Lang: map[int]string{}}
	g := New(Config{Channel: 1}, d, slog.New(slog.DiscardHandler))
	for _, id := range []int{1, 2} {
		u := g.users[id]
		u.status = userPlay
		u.trade = emptyTrade()
		g.mobs[id].mode = 1
		g.mobs[id].info.Player.Status.CurHP = 100
		g.mobs[id].info.Player.Gold = 1000
	}
	return g, g.users[1], g.users[2]
}

func offerOf(to int, gold int64, confirm uint8, items map[int]wire.Item) wire.Trade {
	tr := emptyTrade()
	tr.ClientID, tr.Gold, tr.Confirm = uint16(to), gold, confirm
	n := 0
	for slot, it := range items {
		tr.Item[n], tr.Slot[n] = it, int8(slot)
		n++
	}
	return tr
}

func TestTradeItemsSwap(t *testing.T) {
	g, a, b := tradeGame(t)
	pa, pb := &g.mobs[1].info.Player, &g.mobs[2].info.Player
	sword, shield := wire.Item{Index: 1103}, wire.Item{Index: 1201}
	pa.Inventory[4], pb.Inventory[7] = sword, shield

	send := func(u *User, tr wire.Trade) { g.requestTrade(u, packet(&tr)) }
	send(a, offerOf(2, 0, 0, map[int]wire.Item{4: sword}))
	send(b, offerOf(1, 0, 0, map[int]wire.Item{7: shield}))
	send(a, offerOf(2, 50, 0, map[int]wire.Item{4: sword}))
	send(a, offerOf(2, 50, 1, map[int]wire.Item{4: sword}))
	if a.trade.Confirm != 1 {
		t.Fatal("A's confirm not stored")
	}
	send(b, offerOf(1, 0, 1, map[int]wire.Item{7: shield}))

	if pa.Inventory[4].Index != 0 || pa.Inventory[0] != shield || pb.Inventory[7].Index != 0 || pb.Inventory[0] != sword {
		t.Fatalf("items not swapped: A %v / %v, B %v / %v", pa.Inventory[0], pa.Inventory[4], pb.Inventory[0], pb.Inventory[7])
	}
	if pa.Gold != 950 || pb.Gold != 1050 {
		t.Fatalf("gold A %d B %d", pa.Gold, pb.Gold)
	}
	if a.trade.ClientID != 0 || b.trade.ClientID != 0 {
		t.Fatal("trade state not cleared")
	}
}

func TestTradeCancelsOnTamper(t *testing.T) {
	g, a, b := tradeGame(t)
	pa := &g.mobs[1].info.Player
	pa.Inventory[4] = wire.Item{Index: 1103}
	send := func(u *User, tr wire.Trade) { g.requestTrade(u, packet(&tr)) }
	send(a, offerOf(2, 0, 0, map[int]wire.Item{4: {Index: 1103}}))
	send(b, offerOf(1, 0, 0, nil))
	// A claims a different item in the same slot.
	send(a, offerOf(2, 0, 0, map[int]wire.Item{4: {Index: 1104}}))
	if a.trade.ClientID != 0 || b.trade.ClientID != 0 {
		t.Fatal("tampered offer did not cancel the trade")
	}
	if pa.Inventory[4].Index != 1103 {
		t.Fatal("inventory changed")
	}
}

func TestTradeNoSpace(t *testing.T) {
	g, a, b := tradeGame(t)
	pa, pb := &g.mobs[1].info.Player, &g.mobs[2].info.Player
	for i := range invSlots {
		pb.Inventory[i] = wire.Item{Index: 1201} // B's inventory is full
	}
	pa.Inventory[0] = wire.Item{Index: 1103}
	send := func(u *User, tr wire.Trade) { g.requestTrade(u, packet(&tr)) }
	send(a, offerOf(2, 0, 0, map[int]wire.Item{0: {Index: 1103}}))
	send(b, offerOf(1, 0, 0, nil))
	send(a, offerOf(2, 0, 1, map[int]wire.Item{0: {Index: 1103}}))
	send(b, offerOf(1, 0, 1, nil))
	if pa.Inventory[0].Index != 1103 {
		t.Fatal("item moved although B has no space")
	}
	if b.trade.ClientID == 0 {
		t.Fatal("trade should stay open when there is no space (as in the C++)")
	}
}

func TestTradeOneRequestPerSecond(t *testing.T) {
	g, a, b := tradeGame(t)
	send := func(u *User, tr wire.Trade) { g.requestTrade(u, packet(&tr)) }
	send(a, offerOf(2, 0, 0, nil))
	g.removeTrade(1)
	send(a, offerOf(2, 0, 0, nil))
	if a.trade.ClientID != 0 {
		t.Fatal("second request within a second was accepted")
	}
	_ = b
}
