package server

import (
	"context"
	"testing"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// A notice from the panel reaches every connected channel.
func TestNoticeDelivery(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	wait := func(id int64) string {
		t.Helper()
		var res string
		waitFor(t, func() bool {
			done, r, _ := h.st.DeliveryResult(ctx, id)
			res = r
			return done
		})
		return res
	}

	id, err := h.st.Enqueue(ctx, "notice", NoticeUser, map[string]any{"text": "Manutenção às 22h"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	var n wire.Notice96
	tm.expect(wire.OpServerNotice, &n)
	if got := wire.GameString(n.Text[:]); got != "Manutenção às 22h" {
		t.Fatalf("notice text %q", got)
	}
	if r := wait(id); r != resSentOnline+": 1" {
		t.Fatalf("result %q", r)
	}

	id, _ = h.st.Enqueue(ctx, "notice", NoticeUser, map[string]any{"text": "  "}, "test")
	if r := wait(id); r != resInvalid+": text" {
		t.Fatalf("empty notice result %q", r)
	}
}

// TMSRV's ranking rows are stored; deleted characters drop out of the list.
func TestRankUpdate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	send := func(name string, level int32) {
		p := wire.RankUpdate{Header: wire.NewHeader(wire.OpRankUpdate, 0), Level: level, Power: 7}
		wire.PutGameString(p.Name[:], name)
		p.Equip[15].Index = 543
		tm.send(&p)
	}
	rec, err := h.st.LoadAccount(ctx, "TESTER")
	if err != nil {
		t.Fatal(err)
	}
	name := wire.GameString(rec.Account.Mob[0].Player.Name[:])
	if name == "" {
		t.Fatal("harness account without a character")
	}
	send(name, 50)
	send(name, 51) // overwrites
	send("NaoExiste", 99)
	waitFor(t, func() bool {
		list, err := h.st.Ranking(ctx)
		return err == nil && len(list) == 1 && list[0].Name == name && list[0].Level == 51 && list[0].Equip[15].Index == 543
	})
}

// A GM command from the panel goes to its channel, and the answer is kept.
func TestGMCommandDelivery(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	id, err := h.st.Enqueue(ctx, "gm_command", store.GMCommandUser, map[string]any{"channel": 1, "char_name": "Heroi", "command": "+coin 5"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	var p wire.PanelCommand
	tm.expect(wire.OpPanelCommand, &p)
	if p.Delivery != id || wire.GameString(p.CharName[:]) != "Heroi" || wire.GameString(p.Command[:]) != "+coin 5" {
		t.Fatalf("packet %+v", p)
	}
	r := wire.PanelCommandResult{Header: wire.NewHeader(wire.OpPanelCommandResult, 0), Delivery: id}
	wire.PutGameString(r.Text[:255], "Gold setado")
	tm.send(&r)
	waitFor(t, func() bool {
		list, _ := h.st.GMCommands(ctx, 1)
		return len(list) == 1 && list[0].Done && list[0].Output == "Gold setado"
	})
	offline, _ := h.st.Enqueue(ctx, "gm_command", store.GMCommandUser, map[string]any{"channel": 2, "command": "+on"}, "test")
	waitFor(t, func() bool {
		done, res, _ := h.st.DeliveryResult(ctx, offline)
		return done && res == resNotOnline
	})
}

// /gm reaches the staff's list.
func TestGMMessageStored(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	p := wire.GMMessage{Header: wire.NewHeader(wire.OpGMMessage, 1), Channel: 1}
	wire.PutCString(p.Username[:], "CONTA")
	wire.PutGameString(p.CharName[:15], "Heroi")
	wire.PutGameString(p.Text[:99], "Socorro, não consigo sair")
	tm.send(&p)
	waitFor(t, func() bool {
		list, _ := h.st.GMMessages(ctx, true, 5)
		return len(list) == 1 && list[0].Text == "Socorro, não consigo sair" && list[0].CharName == "Heroi"
	})
}
