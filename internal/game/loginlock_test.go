package game

import (
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The panel's 0xC1F clears the "3 wrong passwords" lock at once.
func TestLoginUnblockClearsFailures(t *testing.T) {
	g := worldGame(t)
	for range 3 {
		failedLogins.add("LOCKED")
	}
	if failedLogins.count("LOCKED") < 3 {
		t.Fatal("account not locked")
	}
	p := wire.LoginUnblock{Header: wire.NewHeader(wire.OpLoginUnblock, 0)}
	wire.PutCString(p.Username[:], "locked")
	raw := wire.MustMarshal(&p)
	g.handleDB(protocol.Packet{Header: protocol.Header{PacketID: wire.OpLoginUnblock}, Raw: raw})
	if n := failedLogins.count("LOCKED"); n != 0 {
		t.Errorf("%d failures left", n)
	}
}
