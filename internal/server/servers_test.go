package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// second starts the DBSRV of another server on the same database.
func (h *harness) second(server int) *harness {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st, err := store.OpenServer(ctx, os.Getenv("DBSRV_TEST_DSN"), testLoc, server)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		h.t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})).With("server", server)
	}
	o := &harness{t: h.t, st: st, addr: ln.Addr().String()}
	o.srv = New(Config{Group: server, ChannelIPs: []string{"127.0.0.1"}, Loc: testLoc, DeliveryInterval: 50 * time.Millisecond}, st, h.srv.base, 8, log)
	if err := o.srv.LoadWorld(ctx); err != nil {
		h.t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = o.srv.Serve(ctx, ln); close(done) }()
	h.t.Cleanup(func() { cancel(); <-done })
	return o
}

// Two servers, one database: the account is the same, the characters are
// not, and it plays on one server at a time.
func TestAccountAcrossServers(t *testing.T) {
	h1 := newHarness(t)
	h2 := h1.second(2)
	ctx := context.Background()
	for _, h := range []*harness{h1, h2} {
		waitFor(t, func() bool { _, seen, _, _ := h.st.DBSRVStatus(ctx); return seen != nil })
	}
	tm1, tm2 := h1.connect(), h2.connect()

	// Server 1: the character is there.
	r := tm1.loginOK(8, "TESTER", "secret")
	if name := wire.GameString(r.CharList.Name[0][:]); name != "Hero" {
		t.Fatalf("server 1 character %q", name)
	}
	tm1.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 8), CharIndex: 0})
	var extra wire.CNFCharacterLoginExtra
	tm1.expect(wire.OpCNFCharacterLoginExtra, &extra)
	tm1.expect(wire.OpCNFCharacterLogin, nil)

	// Server 2 while playing on server 1: refused, and nothing is touched.
	tm2.login(3, "TESTER", "secret", false)
	tm2.expect(wire.OpAlreadyPlaying, nil)
	if on, _ := h2.st.Online(ctx, "TESTER"); on == nil || on.Server != 1 {
		t.Fatalf("online: %+v", on)
	}

	// Cash queued on server 2 goes to the DBSRV of server 1, which has the
	// player, and reaches the game there.
	cashID, err := h2.st.Enqueue(ctx, "cash", "TESTER", map[string]any{"amount": 40}, "test")
	if err != nil {
		t.Fatal(err)
	}
	var cash wire.CashDelivery
	tm1.expect(wire.OpCashDelivery, &cash)
	if cash.Cash != 40 {
		t.Errorf("cash %+v", cash)
	}
	waitFor(t, func() bool { done, res, _ := h1.st.DeliveryResult(ctx, cashID); return done && res == resSentOnline })

	// An edit to the world of server 2 waits for the player to leave server 1.
	editID, err := h2.st.Enqueue(ctx, "edit", "TESTER", map[string]any{"op": "coin", "value": 5}, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		done, res, _ := h2.st.DeliveryResult(ctx, editID)
		return done || res == waitOtherServer
	})
	if done, res, _ := h2.st.DeliveryResult(ctx, editID); done || res != waitOtherServer {
		t.Errorf("edit on server 2 while playing on server 1: done %v result %q", done, res)
	}

	// Logging in on server 2 with "disconnect the other session": the DBSRV
	// of server 1 is asked to save and quit.
	tm2.login(3, "TESTER", "secret", true)
	tm2.expect(wire.OpDisconnectPrevious, nil)
	var sq wire.SavingQuit
	tm1.expect(wire.OpSavingQuit, &sq)
	if wire.CString(sq.AccountName[:]) != "TESTER" {
		t.Errorf("kick packet %+v", sq)
	}
	// TMSRV of server 1 saves (with the cash it received) and quits.
	p := wire.SaveCharacter{Header: wire.NewHeader(wire.OpSaveMobQuit, 8), Mob: extra.Mob, Cash: 40}
	wire.PutCString(p.User[:], "TESTER")
	copy(p.SkillBar[0:4], extra.Mob.Player.SkillBar1[:])
	copy(p.SkillBar[4:], extra.Mob.SkillBar[:])
	tm1.send(&p)
	tm1.expect(wire.OpSaveQuitDone, nil)
	waitFor(t, func() bool { on, _ := h1.st.Online(ctx, "TESTER"); return on == nil })

	// The edit that waited is applied to the world of server 2.
	waitFor(t, func() bool { done, _, _ := h2.st.DeliveryResult(ctx, editID); return done })

	// Now server 2 takes it: same account and cash, an empty world.
	r2 := tm2.loginOK(3, "TESTER", "secret")
	if name := wire.GameString(r2.CharList.Name[0][:]); name != "" {
		t.Errorf("server 2 shows the character of server 1: %q", name)
	}
	if on, _ := h1.st.Online(ctx, "TESTER"); on == nil || on.Server != 2 {
		t.Errorf("online after the switch: %+v", on)
	}
	on2, err := h2.st.LoadAccount(ctx, "TESTER")
	if err != nil {
		t.Fatal(err)
	}
	if on2.Account.StorageCoin != 5 {
		t.Errorf("storage gold on server 2: %d, want the 5 of the edit", on2.Account.StorageCoin)
	}
	if on2.Account.Cash != 40 {
		t.Errorf("cash on server 2: %d, want the 40 delivered on server 1", on2.Account.Cash)
	}
	// And server 1 now refuses it.
	tm1.login(9, "TESTER", "secret", false)
	tm1.expect(wire.OpAlreadyPlaying, nil)

	// The world of server 1 is intact.
	back, err := h1.st.LoadAccount(ctx, "TESTER")
	if err != nil || wire.GameString(back.Account.Mob[0].Player.Name[:]) != "Hero" || back.Account.StorageCoin != 0 {
		t.Errorf("server 1 world changed: %v", err)
	}
}

// Two DBSRVs must never serve the same server.
func TestSecondDBSRVOfTheSameServerIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	release, err := h.st.LockServer(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	other, err := store.Open(ctx, os.Getenv("DBSRV_TEST_DSN"), testLoc)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.LockServer(ctx, nil); err == nil {
		t.Error("a second DBSRV took server 1")
	}
}
