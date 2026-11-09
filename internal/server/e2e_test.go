package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// End-to-end: a fake TMSRV speaks the real protocol over TCP to the server,
// which uses the Postgres test database. Needs DBSRV_TEST_DSN (the database
// is wiped), e.g. postgres://dbsrv:dbsrv@localhost:5432/dbsrv_test.

var testLoc = time.FixedZone("BRT", -3*3600)

type harness struct {
	t    *testing.T
	st   *store.Store
	srv  *Server
	addr string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := os.Getenv("DBSRV_TEST_DSN")
	if dsn == "" {
		t.Skip("DBSRV_TEST_DSN not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	st, err := store.Open(ctx, dsn, testLoc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.ResetForTests(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var base [4]wire.Mob
	for i, n := range []string{"TK", "FM", "BM", "HT"} {
		raw, err := os.ReadFile(filepath.Join("../wire/testdata/npc", n))
		if err != nil {
			t.Fatal(err)
		}
		if err := wire.UnmarshalLegacyMob(raw, &base[i]); err != nil {
			t.Fatal(err)
		}
	}

	h := &harness{t: t, st: st}
	h.seed("TESTER", "secret", base[0], "Hero")
	h.seed("OTHER", "pw2", base[1], "Rival")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	if err := st.UpsertGuild(ctx, store.Guild{ID: 6, Name: "OvErLoRd", Kingdom: 7, Fame: 10}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceDonateStore(ctx, []store.DonateSlot{{Store: 1, Slot: 2, Price: 100, Available: 2, Item: wire.Item{Index: 4140}}}); err != nil {
		t.Fatal(err)
	}
	h.srv = New(Config{ChannelIPs: []string{"127.0.0.1", "127.0.0.1"}, Loc: testLoc, DeliveryInterval: 50 * time.Millisecond}, st, base, 8, log)
	if err := h.srv.LoadWorld(ctx); err != nil {
		t.Fatal(err)
	}
	h.addr = ln.Addr().String()
	done := make(chan struct{})
	go func() { _ = h.srv.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return h
}

func (h *harness) seed(user, pw string, mob wire.Mob, charName string) {
	h.t.Helper()
	hash, err := auth.Hash(pw)
	if err != nil {
		h.t.Fatal(err)
	}
	rec := &store.AccountRecord{PasswordHash: hash}
	wire.PutCString(rec.Account.Username[:], user)
	rec.Account.Mob[0].Player = mob
	wire.PutCString(rec.Account.Mob[0].Player.Name[:], charName)
	rec.Account.Mob[0].Player.Gold = 100
	ctx := context.Background()
	if err := h.st.SaveAccount(ctx, rec); err != nil {
		h.t.Fatal(err)
	}
	if ok, err := h.st.ReserveName(ctx, rec.ID, charName, false); !ok || err != nil {
		h.t.Fatalf("reserve %s: %v %v", charName, ok, err)
	}
}

// tmsrv is a fake game server connection.
type tmsrv struct {
	t  *testing.T
	nc net.Conn
	c  *protocol.Conn
}

func (h *harness) connect() *tmsrv {
	h.t.Helper()
	nc, err := net.Dial("tcp", h.addr)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { nc.Close() })
	tm := &tmsrv{t: h.t, nc: nc, c: protocol.NewDialedConn(nc)}
	if err := tm.c.Hello(); err != nil {
		h.t.Fatal(err)
	}
	var hello wire.Parm3
	tm.expect(wire.OpServerHello, &hello)
	if hello.Parm1 != 1 || hello.Parm2 != 8 {
		h.t.Fatalf("hello %+v", hello)
	}
	return tm
}

func (tm *tmsrv) send(v any) {
	tm.t.Helper()
	if err := tm.c.SendRaw(wire.MustMarshal(v)); err != nil {
		tm.t.Fatal(err)
	}
}

// expect reads the next packet and requires it to be op; v (optional)
// receives the decoded body.
func (tm *tmsrv) expect(op uint16, v any) protocol.Packet {
	tm.t.Helper()
	_ = tm.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
	pkt, err := tm.c.ReadPacket()
	if err != nil {
		tm.t.Fatalf("waiting for 0x%X: %v", op, err)
	}
	if pkt.Header.PacketID != op {
		tm.t.Fatalf("got packet 0x%X, want 0x%X", pkt.Header.PacketID, op)
	}
	if v != nil {
		if err := wire.Unmarshal(pkt.Raw, v); err != nil {
			tm.t.Fatal(err)
		}
	}
	return pkt
}

func (tm *tmsrv) login(client uint16, user, pw string, kick bool) {
	p := wire.AccountLogin{Header: wire.NewHeader(wire.OpAccountLogin, client)}
	wire.PutCString(p.Login[:], user)
	wire.PutCString(p.Password[:], pw)
	if kick {
		p.Unknow84 = 1
	}
	tm.send(&p)
}

func (tm *tmsrv) loginOK(client uint16, user, pw string) wire.CNFAccountLogin {
	tm.t.Helper()
	tm.login(client, user, pw, false)
	var r wire.CNFAccountLogin
	tm.expect(wire.OpCNFAccountLogin, &r)
	tm.expect(wire.OpAdditionalAccountInfo, nil)
	return r
}

func (tm *tmsrv) save(op uint16, client uint16, user string, slot int, mob wire.CharInfo) {
	p := wire.SaveCharacter{Header: wire.NewHeader(op, client), CharSlot: uint32(slot), Mob: mob, Slot: int16(slot)}
	wire.PutCString(p.User[:], user)
	copy(p.SkillBar[0:4], mob.Player.SkillBar1[:])
	copy(p.SkillBar[4:], mob.SkillBar[:])
	tm.send(&p)
}

func TestLoginFlow(t *testing.T) {
	h := newHarness(t)
	tm := h.connect()

	tm.login(5, "nobody", "x", false)
	tm.expect(wire.OpAccountNotFound, nil)

	tm.login(5, "tester", "wrong", false)
	tm.expect(wire.OpWrongPassword, nil)

	r := tm.loginOK(5, "tester", "secret")
	if wire.CString(r.UserName[:]) != "TESTER" || wire.CString(r.CharList.Name[0][:]) != "Hero" || r.CharList.Gold[0] != 100 {
		t.Fatalf("char list: user=%q name=%q gold=%d", wire.CString(r.UserName[:]), wire.CString(r.CharList.Name[0][:]), r.CharList.Gold[0])
	}
	if r.CharList.Equip[0][0].Index != 1 {
		t.Errorf("face %d", r.CharList.Equip[0][0].Index)
	}

	tm.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 5), CharIndex: 0})
	var extra wire.CNFCharacterLoginExtra
	tm.expect(wire.OpCNFCharacterLoginExtra, &extra)
	var cl wire.CNFCharacterLogin
	tm.expect(wire.OpCNFCharacterLogin, &cl)
	if wire.CString(cl.Mob.Name[:]) != "Hero" || cl.SlotIndex != 0 {
		t.Fatalf("character login: %q slot %d", wire.CString(cl.Mob.Name[:]), cl.SlotIndex)
	}

	// Periodic save: gold changes, account stays online.
	mob := extra.Mob
	mob.Player.Gold = 4242
	tm.save(wire.OpSaveMob, 5, "TESTER", 0, mob)
	waitFor(t, func() bool {
		rec, err := h.st.LoadAccount(context.Background(), "TESTER")
		return err == nil && rec.Account.Mob[0].Player.Gold == 4242
	})

	// Save with the wrong account name is refused.
	mob.Player.Gold = 1
	tm.save(wire.OpSaveMob, 5, "OTHER", 0, mob)

	// Save and quit.
	mob.Player.Gold = 5000
	tm.save(wire.OpSaveMobQuit, 5, "TESTER", 0, mob)
	tm.expect(wire.OpSaveQuitDone, nil)
	rec, _ := h.st.LoadAccount(context.Background(), "TESTER")
	if rec.Account.Mob[0].Player.Gold != 5000 {
		t.Errorf("gold after quit = %d, want 5000 (the wrong-name save must not apply)", rec.Account.Mob[0].Player.Gold)
	}

	// Logged out: can log in again.
	tm.loginOK(6, "tester", "secret")
}

func TestKickPreviousSession(t *testing.T) {
	h := newHarness(t)
	ch1, ch2 := h.connect(), h.connect()

	ch1.loginOK(10, "TESTER", "secret")
	ch1.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 10), CharIndex: 0})
	var extra wire.CNFCharacterLoginExtra
	ch1.expect(wire.OpCNFCharacterLoginExtra, &extra)
	ch1.expect(wire.OpCNFCharacterLogin, nil)

	// Same account on channel 2 without the kick flag.
	ch2.login(20, "TESTER", "secret", false)
	ch2.expect(wire.OpAlreadyPlaying, nil)

	// With the kick flag: channel 2 is told to wait, channel 1 to save & quit.
	ch2.login(20, "TESTER", "secret", true)
	ch2.expect(wire.OpDisconnectPrevious, nil)
	var sq wire.SavingQuit
	pkt := ch1.expect(wire.OpSavingQuit, &sq)
	if wire.CString(sq.AccountName[:]) != "TESTER" || pkt.Header.ClientID != 10 {
		t.Fatalf("saving quit %+v client %d", sq, pkt.Header.ClientID)
	}
	extra.Mob.Player.Gold = 777
	ch1.save(wire.OpSaveMobQuit, 10, "TESTER", 0, extra.Mob)
	ch1.expect(wire.OpSaveQuitDone, nil)

	// The retry sees the data saved by channel 1.
	r := ch2.loginOK(20, "TESTER", "secret")
	if r.CharList.Gold[0] != 777 {
		t.Errorf("gold after handover %d", r.CharList.Gold[0])
	}
}

func TestDisconnectFreesSessions(t *testing.T) {
	h := newHarness(t)
	ch1 := h.connect()
	ch1.loginOK(3, "TESTER", "secret")
	ch1.nc.Close()

	ch2 := h.connect()
	waitFor(t, func() bool { return h.srv.sess.byName("TESTER") == nil })
	ch2.loginOK(4, "TESTER", "secret")
}

func TestCharacterLifecycle(t *testing.T) {
	h := newHarness(t)
	tm := h.connect()
	tm.loginOK(7, "TESTER", "secret")

	newChar := func(slot uint32, name string, class uint32) {
		p := wire.NewCharacter{Header: wire.NewHeader(wire.OpNewCharacter, 7), SlotID: slot, ClassID: class}
		wire.PutCString(p.Nick[:], name)
		tm.send(&p)
	}

	newChar(1, "Rival", 1) // owned by OTHER
	tm.expect(wire.OpNewCharFail, nil)
	newChar(1, "bad name", 1)
	tm.expect(wire.OpNewCharFail, nil)
	newChar(1, "COM1", 1)
	tm.expect(wire.OpNewCharFail, nil)
	newChar(0, "Fresh", 1) // slot occupied
	tm.expect(wire.OpNewCharFail, nil)

	newChar(1, "Fresh-1", 2)
	var cl wire.CharListPacket
	tm.expect(wire.OpCNFNewCharacter, &cl)
	if wire.CString(cl.CharList.Name[1][:]) != "Fresh-1" || cl.CharList.Name[0][0] == 0 {
		t.Fatalf("names after create: %q %q", cl.CharList.Name[0][:], cl.CharList.Name[1][:])
	}
	if face := cl.CharList.Equip[1][0].Index; face != 21 {
		t.Errorf("BM face %d, want 21", face)
	}
	if owner, _ := h.st.NameOwner(context.Background(), "fresh-1"); owner != "TESTER" {
		t.Errorf("name owner %q", owner)
	}

	// Arch in slot 2 with the mortal's name.
	arch := wire.NewArch{Header: wire.NewHeader(wire.OpNewArch, 7), PosID: 2, ClassID: 26, ClassInfo: 2, MortalSlot: 1}
	wire.PutCString(arch.Name[:], "Fresh-1")
	tm.send(&arch)
	tm.expect(wire.OpCNFNewCharacter, &cl)
	if wire.CString(cl.CharList.Name[2][:]) != "Fresh-1" || cl.CharList.Equip[2][0].Index != 26 {
		t.Fatalf("arch: %q face %d", cl.CharList.Name[2][:], cl.CharList.Equip[2][0].Index)
	}

	del := func(slot int32, pw string) {
		p := wire.DeleteCharacter{Header: wire.NewHeader(wire.OpDeleteCharacter, 7), SlotIndex: slot}
		wire.PutCString(p.Pwd[:], pw)
		tm.send(&p)
	}
	del(1, "nope")
	tm.expect(wire.OpAlreadyPlaying, nil) // wrong password code

	// Delete the mortal: the Arch still uses the name, so it stays reserved.
	del(1, "secret")
	tm.expect(wire.OpCNFDeleteCharacter, &cl)
	if cl.CharList.Name[1][0] != 0 {
		t.Fatal("slot 1 not cleared")
	}
	if owner, _ := h.st.NameOwner(context.Background(), "fresh-1"); owner != "TESTER" {
		t.Error("name released while the Arch still has it")
	}

	// Delete the Arch (evolution 2): now the name is free.
	del(2, "secret")
	tm.expect(wire.OpCNFDeleteCharacter, &cl)
	if _, err := h.st.NameOwner(context.Background(), "fresh-1"); err != store.ErrNotFound {
		t.Errorf("name still reserved: %v", err)
	}
}

func TestNumericPassword(t *testing.T) {
	h := newHarness(t)
	tm := h.connect()
	tm.loginOK(9, "TESTER", "secret")

	req := func(num string, change int32) {
		p := wire.NumericPassword{Header: wire.NewHeader(wire.OpNumericPassword, 9), RequestChange: change}
		wire.PutCString(p.Num[:], num)
		tm.send(&p)
	}
	req("1234", 0) // first use sets it
	tm.expect(wire.OpNumericPassword, nil)
	req("9999", 0)
	tm.expect(wire.OpNumericPasswordWrong, nil)
	req("1234", 0)
	tm.expect(wire.OpNumericPassword, nil)
	req("5555", 1) // change
	tm.expect(wire.OpNumericPassword, nil)

	rec, _ := h.st.LoadAccount(context.Background(), "TESTER")
	if ok, _ := auth.Verify(rec.NumericPassHash, "5555"); !ok {
		t.Error("changed numeric password not persisted")
	}
}

func TestBanAndStatus(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	rec, _ := h.st.LoadAccount(ctx, "TESTER")
	future := time.Now().In(testLoc).Add(48 * time.Hour)
	rec.Account.BanType = 2
	rec.Account.Ban = wire.ItemDate{Dia: uint8(future.Day()), Mes: uint8(future.Month()), Ano: uint16(future.Year())}
	if err := h.st.SaveAccount(ctx, rec); err != nil {
		t.Fatal(err)
	}
	other, _ := h.st.LoadAccount(ctx, "OTHER")
	other.Status = auth.StatusBlocked
	if err := h.st.SaveAccount(ctx, other); err != nil {
		t.Fatal(err)
	}

	tm := h.connect()
	tm.login(1, "TESTER", "secret", false)
	tm.expect(wire.OpAccountDisabled, nil) // ban type 2 -> 0x425
	tm.login(2, "OTHER", "pw2", false)
	tm.expect(wire.OpAccountBlocked, nil)

	// An expired ban is lifted at login.
	past := time.Now().In(testLoc).Add(-48 * time.Hour)
	rec.Account.Ban = wire.ItemDate{Dia: uint8(past.Day()), Mes: uint8(past.Month()), Ano: uint16(past.Year())}
	if err := h.st.SaveAccount(ctx, rec); err != nil {
		t.Fatal(err)
	}
	tm.loginOK(1, "TESTER", "secret")
}

// changeLogin is the client's login on the new channel: the 0x52A key in
// Unknow, no password, kick flag set (TMFieldScene::OnPacketCNFRemoveServer).
func (tm *tmsrv) changeLogin(client uint16, user, key string) {
	p := wire.AccountLogin{Header: wire.NewHeader(wire.OpAccountLogin, client), Unknow84: 1}
	wire.PutCString(p.Login[:], user)
	wire.PutCString(p.Unknow[:], key)
	tm.send(&p)
}

func (tm *tmsrv) askChange(client uint16, to int32) string {
	tm.t.Helper()
	tm.send(&wire.ServerChange{Header: wire.NewHeader(wire.OpServerChange, client), NewServerID: to})
	var r wire.AccountLogin
	tm.expect(wire.OpReLoginForChange, &r)
	if wire.CString(r.Login[:]) != "TESTER" || wire.CString(r.Password[:]) != "" {
		tm.t.Fatalf("0x52A must carry the account and no password: %+v", r)
	}
	return wire.CString(r.Unknow[:])
}

func TestChannelChange(t *testing.T) {
	h := newHarness(t)
	ch1, ch2 := h.connect(), h.connect() // channels 1 and 2 (indexes 0, 1)
	ch1.loginOK(11, "TESTER", "secret")
	if key := ch1.askChange(11, 2); key != "" {
		t.Fatalf("change from the character list accepted: %q", key)
	}
	ch1.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 11), CharIndex: 0})
	var extra wire.CNFCharacterLoginExtra
	ch1.expect(wire.OpCNFCharacterLoginExtra, &extra)
	ch1.expect(wire.OpCNFCharacterLogin, nil)

	for _, to := range []int32{1, 3, 0, -1} { // own channel, not configured, invalid
		if key := ch1.askChange(11, to); key != "" {
			t.Errorf("change to %d accepted: %q", to, key)
		}
	}
	key := ch1.askChange(11, 2)
	if len(key) < 30 || key[:3] != "*2 " {
		t.Fatalf("key %q, want \"*2 <token>\"", key)
	}

	// The client reconnects before the old channel's save-and-quit lands:
	// the login waits for it instead of failing as "already playing".
	ch2.changeLogin(21, "TESTER", key)
	time.Sleep(100 * time.Millisecond)
	extra.Mob.Player.Gold = 4321
	p := wire.SaveCharacter{Header: wire.NewHeader(wire.OpSaveMobQuit, 11), Mob: extra.Mob, Position: wire.Position{X: 2150, Y: 2077}}
	wire.PutCString(p.User[:], "TESTER")
	ch1.send(&p)
	ch1.expect(wire.OpSaveQuitDone, nil)

	ch2.expect(wire.OpCNFAccountLogin, nil)
	ch2.expect(wire.OpAdditionalAccountInfo, nil)
	var extra2 wire.CNFCharacterLoginExtra
	ch2.expect(wire.OpCNFCharacterLoginExtra, &extra2)
	var cl wire.CNFCharacterLogin
	ch2.expect(wire.OpCNFCharacterLogin, &cl)
	if cl.WorldPos != (wire.Position{X: 2150, Y: 2077}) || cl.SlotIndex != 0 || cl.Mob.Gold != 4321 {
		t.Errorf("entered with pos %+v slot %d gold %d", cl.WorldPos, cl.SlotIndex, cl.Mob.Gold)
	}
	if on, _ := h.st.Online(context.Background(), "TESTER"); on == nil || on.Channel != 1 {
		t.Errorf("online row %+v, want channel index 1", on)
	}

	// The key works once.
	ch2.send(&wire.Signal{Header: wire.NewHeader(wire.OpNoNeedSave, 21)})
	waitFor(t, func() bool { return h.srv.sess.byName("TESTER") == nil })
	ch2.changeLogin(21, "TESTER", key)
	ch2.expect(wire.OpWrongPassword, nil)

	// A key for channel 2 is refused on channel 1, and is then gone.
	ch2.loginOK(22, "TESTER", "secret")
	ch2.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 22), CharIndex: 0})
	ch2.expect(wire.OpCNFCharacterLoginExtra, nil)
	ch2.expect(wire.OpCNFCharacterLogin, nil)
	key = ch2.askChange(22, 1)
	ch2.send(&wire.Signal{Header: wire.NewHeader(wire.OpNoNeedSave, 22)})
	waitFor(t, func() bool { return h.srv.sess.byName("TESTER") == nil })
	ch2.changeLogin(23, "TESTER", "*2 "+key[3:])
	ch2.expect(wire.OpWrongPassword, nil)
	ch1.changeLogin(12, "TESTER", key)
	ch1.expect(wire.OpWrongPassword, nil)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
