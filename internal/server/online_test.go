package server

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// The user's report: gold edited in SQL while the player was online was
// overwritten by the next TMSRV save. Now the edit is refused while online,
// "kick" makes TMSRV save and disconnect, and then the edit sticks.
func TestDirectEditGuard(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	operator, err := store.OpenUnprivileged(ctx, os.Getenv("DBSRV_TEST_DSN"), testLoc) // like psql or pgAdmin
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(operator.Close)
	setGold := func(gold int) error {
		_, err := operator.Pool().Exec(ctx, `
			UPDATE characters SET gold = $1
			WHERE account_id = (SELECT id FROM accounts WHERE username = 'TESTER') AND slot = 0`, gold)
		return err
	}
	waitFor(t, func() bool { _, seen, _, _ := h.st.DBSRVStatus(ctx); return seen != nil })

	tm := h.connect()
	tm.loginOK(8, "TESTER", "secret")
	tm.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 8), CharIndex: 0})
	var extra wire.CNFCharacterLoginExtra
	tm.expect(wire.OpCNFCharacterLoginExtra, &extra)
	tm.expect(wire.OpCNFCharacterLogin, nil)

	if on, _ := h.st.Online(ctx, "TESTER"); on == nil || on.Client != 8 || on.Channel != 0 {
		t.Fatalf("online row: %+v", on)
	}

	// Edit while online: refused with a clear message.
	err = setGold(999999)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55006" || !strings.Contains(pgErr.Message, "TESTER") {
		t.Fatalf("edit while online should fail with object_in_use, got %v", err)
	}
	// dbsrv's own writes still go through.
	extra.Mob.Player.Gold = 4321
	tm.save(wire.OpSaveMob, 8, "TESTER", 0, extra.Mob)
	waitFor(t, func() bool {
		rec, _ := h.st.LoadAccount(ctx, "TESTER")
		return rec != nil && rec.Account.Mob[0].Player.Gold == 4321
	})

	// Kick: TMSRV gets 0x40A mode 2, saves and quits.
	id, err := h.st.Enqueue(ctx, "kick", "TESTER", map[string]any{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	var sq wire.SavingQuit
	tm.expect(wire.OpSavingQuit, &sq)
	if sq.Mode != 2 || wire.CString(sq.AccountName[:]) != "TESTER" {
		t.Errorf("kick packet %+v", sq)
	}
	tm.save(wire.OpSaveMobQuit, 8, "TESTER", 0, extra.Mob)
	tm.expect(wire.OpSaveQuitDone, nil)
	waitFor(t, func() bool { done, _, _ := h.st.DeliveryResult(ctx, id); return done })
	if on, _ := h.st.Online(ctx, "TESTER"); on != nil {
		t.Fatalf("still marked online after logout: %+v", on)
	}

	// Offline: the edit works and the next login sees it.
	if err := setGold(777777); err != nil {
		t.Fatalf("edit while offline: %v", err)
	}
	r := tm.loginOK(9, "TESTER", "secret")
	if r.CharList.Gold[0] != 777777 {
		t.Errorf("gold after re-login %d, want 777777", r.CharList.Gold[0])
	}

	// Kicking someone offline is harmless.
	id, _ = h.st.Enqueue(ctx, "kick", "OTHER", map[string]any{}, "test")
	waitFor(t, func() bool { done, res, _ := h.st.DeliveryResult(ctx, id); return done && res == resNotOnline })
}

// If dbsrv dies its sessions are gone, so stale online rows must not block edits.
func TestGuardIgnoresStaleRowsWhenDBSRVIsDown(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	tm.loginOK(5, "TESTER", "secret")

	if _, err := h.st.Pool().Exec(ctx, `UPDATE server_state SET dbsrv_seen_at = now() - interval '5 minutes'`); err != nil {
		t.Fatal(err)
	}
	operator, err := store.OpenUnprivileged(ctx, os.Getenv("DBSRV_TEST_DSN"), testLoc)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	if _, err := operator.Pool().Exec(ctx, `UPDATE accounts SET cash = 5 WHERE username = 'TESTER'`); err != nil {
		t.Errorf("edit with dbsrv down: %v", err)
	}
}

// A TMSRV disconnect clears its rows; a dbsrv restart clears all.
func TestOnlineRowsClearedOnDisconnect(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	tm.loginOK(5, "TESTER", "secret")
	tm.nc.Close()
	waitFor(t, func() bool { on, _ := h.st.Online(ctx, "TESTER"); return on == nil })
}
