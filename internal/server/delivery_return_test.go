package server

import (
	"context"
	"strings"
	"testing"

	"dbsrv/internal/wire"
)

// An item TMSRV could not place (0xC12) is queued again: kept pending while
// the player is online, put in the stored storage once offline.
func TestItemDeliveryReturn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	tm.loginOK(3, "TESTER", "secret")

	back := func(user string, id uint16) {
		p := wire.ItemDelivery{Header: wire.NewHeader(wire.OpItemDeliveryReturn, 0), Item: wire.Item{Index: id}}
		p.Item.Effect[0] = wire.ItemEffect{Index: 61, Value: 3}
		wire.PutCString(p.Username[:], user)
		tm.send(&p)
	}
	last := func(user string) (result string, done bool) {
		list, err := h.st.RecentDeliveries(ctx, user, 1)
		if err != nil || len(list) == 0 {
			return "", false
		}
		return list[0].Result, list[0].ProcessedAt != nil
	}

	// Online: waits for the logout.
	back("TESTER", 1103)
	waitFor(t, func() bool { r, done := last("TESTER"); return !done && r == waitOffline })

	// Offline: straight into the stored storage.
	back("OTHER", 1104)
	waitFor(t, func() bool { r, done := last("OTHER"); return done && strings.HasPrefix(r, resOK) })
	rec, err := h.st.LoadAccount(ctx, "OTHER")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range rec.Account.StorageItems {
		found = found || (it.Index == 1104 && it.Effect[0] == wire.ItemEffect{Index: 61, Value: 3})
	}
	if !found {
		t.Fatal("returned item not in the offline storage")
	}
}
