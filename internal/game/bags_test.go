package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

// The 60 inventory slots are open without a bag, and every character gets
// permanent bags in slots 60/61 so the client shows the extra pages.
func TestBagsOpenToEveryone(t *testing.T) {
	var pl wire.Mob
	for i := range 30 {
		pl.Inventory[i] = wire.Item{Index: 1}
	}
	if s := firstEmptySlot(&pl); s != 30 {
		t.Fatalf("first free slot %d, want 30 without a bag", s)
	}
	if !invSlotUsable(&pl, 59) || invSlotUsable(&pl, 60) {
		t.Fatal("slot 59 should be usable, 60 (the bag) not")
	}
	if !openBags(&pl) || pl.Inventory[60].Index != itemBag || pl.Inventory[61].Index != itemBag || !bagActive(&pl.Inventory[60]) {
		t.Fatalf("bags not given: %+v %+v", pl.Inventory[60], pl.Inventory[61])
	}
	if openBags(&pl) {
		t.Fatal("bags given twice")
	}
	// Something else in a bag slot is left alone.
	pl.Inventory[61] = wire.Item{Index: 1103}
	openBags(&pl)
	if pl.Inventory[61].Index != 1103 {
		t.Fatal("bag slot item replaced")
	}
}

// WPE warning from the client protection: alert to DBSRV, connection closed.
func TestWPEWarnClosesPlayer(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	u.conn = &clientConn{out: make(chan [][]byte, 8)}
	wire.PutCString(u.account.Username[:], "TESTER")
	g.handleClient(u, packetRaw(wire.MustMarshal(&wire.Signal{Header: wire.NewHeader(opCliWPEWarn, 1)})))
	if u.status == userPlay {
		t.Fatal("player still in game after a WPE warning")
	}
}

// A full send buffer disconnects the player once (closeUser used to flush
// into the same full buffer and call itself until the stack overflowed).
func TestFullSendBufferDisconnectsOnce(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	u.conn = &clientConn{out: make(chan [][]byte)} // never drained: enqueue fails
	g.clientMessage(u, "x")
	g.flush(u)
	if u.conn != nil || u.status == userPlay {
		t.Fatalf("still connected: status %d", u.status)
	}
}

// bagActive (test helper) is the C++ TimeRemaining check on the extra bag in Inventory[60]
// or [61] (EFV1..3 = day, month, year-1900).
func bagActive(it *wire.Item) bool {
	day, month, year := int(it.Effect[0].Value), int(it.Effect[1].Value), int(it.Effect[2].Value)+1900
	end := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	return time.Until(end) > 0
}
