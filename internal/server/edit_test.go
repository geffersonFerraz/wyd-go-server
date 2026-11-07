package server

import (
	"context"
	"testing"

	"dbsrv/internal/acctedit"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func TestEditDeliveries(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	waitFor(t, func() bool {
		proto, seen, _, _ := h.st.DBSRVStatus(ctx)
		return proto == store.DeliveryProtocol && seen != nil
	})
	run := func(user string, e acctedit.Edit) (int64, string) {
		t.Helper()
		id, err := h.st.Enqueue(ctx, "edit", user, e, "test")
		if err != nil {
			t.Fatal(err)
		}
		var res string
		waitFor(t, func() bool {
			done, r, _ := h.st.DeliveryResult(ctx, id)
			res = r
			return done
		})
		return id, res
	}

	// Offline: straight into the database.
	if _, r := run("OTHER", acctedit.Edit{Op: acctedit.OpGold, Value: 5000}); r != resOK {
		t.Fatalf("offline gold: %s", r)
	}
	if _, r := run("OTHER", acctedit.Edit{Op: acctedit.OpLevel, Value: 50}); r != resOK {
		t.Fatalf("offline level: %s", r)
	}
	if _, r := run("OTHER", acctedit.Edit{Op: acctedit.OpAccess, Value: 2}); r != resOK {
		t.Fatalf("offline access: %s", r)
	}
	if _, r := run("OTHER", acctedit.Edit{Op: acctedit.OpItem, Where: wire.SlotInv, Slot: 2,
		Item: []int{4140, 61, 5, 0, 0, 0, 0}}); r != resOK {
		t.Fatalf("offline item: %s", r)
	}
	if _, r := run("OTHER", acctedit.Edit{Op: acctedit.OpGold, Char: 3, Value: 1}); r != acctedit.ResNoCharacter {
		t.Fatalf("missing character: %s", r)
	}
	rec, _ := h.st.LoadAccount(ctx, "OTHER")
	pl := &rec.Account.Mob[0].Player
	if pl.Gold != 5000 || pl.BStatus.Level != 49 || rec.Account.AccessLevel != 2 || pl.Inventory[2].Index != 4140 {
		t.Fatalf("offline edits: gold %d level %d access %d inv %+v", pl.Gold, pl.BStatus.Level, rec.Account.AccessLevel, pl.Inventory[2])
	}

	// At character selection: a character edit goes into DBSRV's copy, the
	// storage waits.
	tm := h.connect()
	tm.loginOK(3, "TESTER", "secret")
	if _, r := run("TESTER", acctedit.Edit{Op: acctedit.OpGold, Value: 4242}); r != resOK {
		t.Fatalf("char select gold: %s", r)
	}
	coinID, err := h.st.Enqueue(ctx, "edit", "TESTER", acctedit.Edit{Op: acctedit.OpCoin, Value: 31}, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		done, r, _ := h.st.DeliveryResult(ctx, coinID)
		return !done && r == waitCharSelected
	})

	// In game: TMSRV gets the 0xC11 and the character carries the gold set above.
	tm.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 3), CharIndex: 0})
	tm.expect(wire.OpCNFCharacterLoginExtra, nil)
	var cl wire.CNFCharacterLogin
	tm.expect(wire.OpCNFCharacterLogin, &cl)
	if cl.Mob.Gold != 4242 {
		t.Errorf("character entered with gold %d", cl.Mob.Gold)
	}
	var coin wire.AdminEdit
	tm.expect(wire.OpAdminEdit, &coin)
	if coin.Op != 5 || coin.Value != 31 || wire.CString(coin.Username[:]) != "TESTER" {
		t.Errorf("coin edit %+v", coin)
	}
	_, r := run("TESTER", acctedit.Edit{Op: acctedit.OpLevel, Value: 10})
	var lvl wire.AdminEdit
	tm.expect(wire.OpAdminEdit, &lvl)
	if r != resSentOnline || lvl.Op != 2 || lvl.Value != 10 {
		t.Errorf("level online: %s %+v", r, lvl)
	}
	// Access: database and TMSRV.
	if _, r := run("TESTER", acctedit.Edit{Op: acctedit.OpAccess, Value: 1}); r != resSentOnline {
		t.Errorf("access online: %s", r)
	}
	tm.expect(wire.OpAdminEdit, nil)
	rec, _ = h.st.LoadAccount(ctx, "TESTER")
	if rec.Account.AccessLevel != 1 {
		t.Errorf("access not stored: %d", rec.Account.AccessLevel)
	}
}
