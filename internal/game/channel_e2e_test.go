package game_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func (cl *client) command(cmd, value string) {
	p := wire.Command{Header: wire.NewHeader(wire.OpCliCommand, 0)}
	wire.PutCString(p.Command[:], cmd)
	wire.PutCString(p.Value[:], value)
	cl.send(&p)
}

// closed waits for the server to close the connection.
func (cl *client) closed() bool {
	_ = cl.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, err := cl.c.ReadPacket()
		if err == nil {
			continue
		}
		var ne net.Error
		return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || !(errors.As(err, &ne) && ne.Timeout())
	}
}

// The in-game channel change: "srv507 2" on channel 1 ends with the
// character in the world of channel 2, where it stood, without the
// character list or the numeric password.
func TestChannelChangeTransparent(t *testing.T) {
	ch := newChainN(t, 2)
	ctx := context.Background()

	a := ch.connectTo(1)
	a.login("PLAYERA", "pwa", "1234")
	w1, _ := a.enter()

	a.command("srv507", "1") // already there
	a.command("srv507", "9") // no such channel
	a.command("srv507", "2")
	var r wire.CNFRemoveServer
	pkt := a.waitFor(wire.OpReLoginForChange, &r)
	key := wire.CString(r.TID[:])
	if !strings.HasPrefix(key, "*2 ") || wire.CString(r.AccountName[:]) != "PLAYERA" || int16(pkt.Header.ClientID) != w1.ClientIndex {
		t.Fatalf("0x52A to the client: id %d account %q key %q", pkt.Header.ClientID, r.AccountName[:], key)
	}
	if !a.closed() {
		t.Fatal("channel 1 kept the connection open")
	}

	// What TMFieldScene::OnPacketCNFRemoveServer sends to the new channel.
	b := ch.connectTo(2)
	login := wire.AccountLogin{Header: wire.NewHeader(wire.OpCliLogin, 0), Unknow84: 1, Mac: [8]byte{1, 2, 3, 7}}
	wire.PutCString(login.Login[:], "PLAYERA")
	wire.PutCString(login.Unknow[:], key)
	b.send(&login)
	b.waitFor(wire.OpSrvCharList, nil)
	var w2 wire.CNFCharacterLogin
	b.waitFor(wire.OpSrvEnterWorld, &w2)
	if wire.CString(w2.Mob.Name[:]) != "Alpha" || w2.WorldPos != w1.WorldPos {
		t.Errorf("entered channel 2 as %q at %+v, was at %+v", w2.Mob.Name[:], w2.WorldPos, w1.WorldPos)
	}
	on, err := ch.st.Online(ctx, "PLAYERA")
	if err != nil || on == nil || on.Channel != 1 {
		t.Errorf("online row %+v err %v, want channel index 1", on, err)
	}

	// The key was used up: replaying it is refused.
	c := ch.connectTo(2)
	c.send(&login)
	c.waitFor(wire.OpSrvLoginFail, nil)
}
