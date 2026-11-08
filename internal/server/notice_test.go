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
