package game

import (
	"testing"

	"dbsrv/internal/wire"
)

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
