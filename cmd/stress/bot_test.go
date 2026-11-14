package main

import (
	"context"
	"net"
	"testing"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

func TestBotLoginCreateEnterAndWalk(t *testing.T) {
	key, err := protocol.GenerateTransportKey()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	got := make(chan uint16, 64)
	served := make(chan struct{})
	go func() {
		defer close(served)
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		pc := protocol.NewSecureConn(nc, key)
		pc.Clock = func() uint32 { return 500000 }
		send := func(v any) {
			if err := pc.SendRaw(wire.MustMarshal(v)); err != nil {
				t.Error(err)
			}
		}
		for {
			pkt, err := pc.ReadPacket()
			if err != nil {
				return
			}
			got <- pkt.Header.PacketID
			switch pkt.Header.PacketID {
			case wire.OpCliLogin:
				var p wire.AccountLogin
				_ = wire.Unmarshal(pkt.Raw, &p)
				if wire.CString(p.Login[:]) != "BOT0001" || wire.CString(p.Password[:]) != "1234" {
					t.Errorf("login %q / %q", p.Login, p.Password)
				}
				r := wire.CNFAccountLogin{Header: wire.NewHeader(wire.OpSrvCharList, wire.ClientIDLogin)}
				wire.PutCString(r.UserName[:], "BOT0001")
				send(&r)
			case wire.OpCliNumericPass:
				send(&wire.NumericPassword{Header: wire.NewHeader(wire.OpNumericPassword, 0)})
			case wire.OpCliCreateChar:
				var p wire.NewCharacter
				_ = wire.Unmarshal(pkt.Raw, &p)
				if wire.CString(p.Nick[:]) != "BOT0001" || p.ClassID != 2 {
					t.Errorf("create %q class %d", p.Nick, p.ClassID)
				}
				send(&wire.CharListPacket{Header: wire.NewHeader(wire.OpSrvCharListNew, wire.ClientIDCharList)})
			case wire.OpCliEnterWorld:
				send(&wire.CNFCharacterLogin{
					Header:      wire.NewHeader(wire.OpSrvEnterWorld, wire.ClientIDCharList),
					WorldPos:    wire.Position{X: 2100, Y: 2100},
					ClientIndex: 7,
				})
			case wire.OpCliMove:
				var a wire.Action
				_ = wire.Unmarshal(pkt.Raw, &a)
				if a.LastPos != (wire.Position{X: 2100, Y: 2100}) {
					t.Errorf("first step from %+v", a.LastPos)
				}
				if ts := a.Header.TimeStamp; ts < 500000 || ts > 515000 {
					t.Errorf("step timestamp %d is not on the server clock", ts)
				}
				return
			}
		}
	}()

	cfg := &botConfig{
		addr: ln.Addr().String(), serverKey: key.PublicKey(), password: "1234", numeric: "123456",
		class: 2, move: 50 * time.Millisecond, wander: 5, stepTimeout: 5 * time.Second,
	}
	st := newStats()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	newBot(cfg, st, "BOT0001").live(ctx)

	<-served
	close(got)
	var seq []uint16
	for id := range got {
		seq = append(seq, id)
	}
	want := []uint16{wire.OpCliLogin, wire.OpCliNumericPass, wire.OpCliCreateChar, wire.OpCliEnterWorld, wire.OpCliMove}
	if len(seq) != len(want) {
		t.Fatalf("bot sent %X, want %X", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("bot sent %X, want %X", seq, want)
		}
	}
	if st.enter.summary() == "-" || st.created.Load() != 1 || st.inWorld.Load() != 0 || st.dropped.Load() != 1 {
		t.Errorf("stats: enter %s created %d inWorld %d dropped %d", st.enter.summary(), st.created.Load(), st.inWorld.Load(), st.dropped.Load())
	}
}
