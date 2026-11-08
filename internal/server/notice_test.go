package server

import (
	"context"
	"testing"

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
