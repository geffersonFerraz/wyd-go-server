package game_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/cluster"
	"dbsrv/internal/game"
	"dbsrv/internal/protocol"
	"dbsrv/internal/server"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Full chain: fake game client -> Go TMSRV -> Go DBSRV (TCP) -> Postgres.
// Needs DBSRV_TEST_DSN and the C++ TMSRV data folder (ItemList.csv etc.).

const tmsrvDir = "../../data/tmsrv"

var loc = time.FixedZone("BRT", -3*3600)

type chain struct {
	t      *testing.T
	st     *store.Store
	tmAddr string   // channel 1
	addrs  []string // every channel, index = number-1
}

func newChain(t *testing.T) *chain { return newChainN(t, 1) }

// newChainN runs one DBSRV and n TMSRVs (channel i on 127.0.0.i, which
// DBSRV tells apart by that source IP).
func newChainN(t *testing.T, n int) *chain {
	t.Helper()
	dsn := os.Getenv("DBSRV_TEST_DSN")
	if dsn == "" {
		t.Skip("DBSRV_TEST_DSN not set")
	}
	if _, err := os.Stat(filepath.Join(tmsrvDir, "NPCGener.txt")); err != nil {
		t.Skipf("TMSRV data not available: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	st, err := store.Open(ctx, dsn, loc)
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
	c := &chain{t: t, st: st}
	c.seed("PLAYERA", "pwa", base[0], "Alpha")
	c.seed("PLAYERB", "pwb", base[2], "Bravo")

	// DBSRV
	dbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	layout := cluster.Config{ServerList: cluster.ServerList{Group: 1, PublicIP: "127.0.0.1", StatusURL: "http://127.0.0.1/serv00.htm"}}
	for i := 1; i <= n; i++ {
		layout.Channels = append(layout.Channels, cluster.Channel{Number: i, IP: fmt.Sprintf("127.0.0.%d", i), Port: 8280 + i})
	}
	db := server.New(server.Config{ChannelIPs: layout.ChannelIPs(), Loc: loc, DeliveryInterval: time.Hour}, st, base, 8, log)
	if err := db.LoadWorld(ctx); err != nil {
		t.Fatal(err)
	}
	go func() { _ = db.Serve(ctx, dbLn) }()

	// Game data goes through Postgres like in production.
	gd := game.GameDataFixture(t)
	if err := st.ImportGameData(ctx, gd, "test"); err != nil {
		t.Fatal(err)
	}
	back, err := st.LoadGameData(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// TMSRVs
	var running []chan struct{}
	for i, chn := range layout.Channels {
		cfg, err := game.LoadConfig(tmsrvDir)
		if err != nil {
			t.Fatal(err)
		}
		cfg.ListenIP, cfg.Channel, cfg.Cluster = chn.IP, chn.Number, layout
		host, port, _ := net.SplitHostPort(dbLn.Addr().String())
		cfg.DBIP = host
		fmt.Sscan(port, &cfg.DBPort)
		data, err := game.LoadData(tmsrvDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := data.ApplyGameData(back); err != nil {
			t.Fatal(err)
		}
		g := game.New(cfg, data, log)
		g.ConnectDB(ctx)
		tmLn, err := net.Listen("tcp", net.JoinHostPort(chn.IP, "0"))
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = g.ServeClients(ctx, tmLn) }()
		done := make(chan struct{})
		go func() { g.Run(ctx); close(done) }()
		running = append(running, done)
		c.addrs = append(c.addrs, tmLn.Addr().String())

		// Wait for this TMSRV's DBSRV link.
		deadline := time.Now().Add(5 * time.Second)
		for !db.ChannelConnected(i) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !db.ChannelConnected(i) {
			t.Fatalf("channel %d did not link to DBSRV", chn.Number)
		}
	}
	t.Cleanup(func() {
		cancel()
		for _, d := range running {
			<-d
		}
	})
	c.tmAddr = c.addrs[0]
	time.Sleep(200 * time.Millisecond)
	return c
}

func (c *chain) seed(user, pw string, mob wire.Mob, name string) {
	c.t.Helper()
	ctx := context.Background()
	hash, _ := auth.Hash(pw)
	rec := &store.AccountRecord{PasswordHash: hash}
	wire.PutCString(rec.Account.Username[:], user)
	m := &rec.Account.Mob[0]
	m.Player = mob
	wire.PutCString(m.Player.Name[:], name)
	m.Player.Equip[0].Effect[1] = wire.ItemEffect{Index: uint8(mob.Equip[0].Index), Value: 1} // mortal, as 0x802 creates
	m.Player.Status = mob.BStatus
	m.Player.Status.Move = 0x02 // speed 2
	m.Player.Status.CurHP = 100
	m.Player.Gold = 500
	m.Player.Inventory[0] = wire.Item{Index: 1103} // a starting item to move around
	if err := c.st.SaveAccount(ctx, rec); err != nil {
		c.t.Fatal(err)
	}
	if ok, err := c.st.ReserveName(ctx, rec.ID, name, false); !ok || err != nil {
		c.t.Fatal(err)
	}
}

type client struct {
	t  *testing.T
	nc net.Conn
	c  *protocol.Conn
}

func (ch *chain) connect() *client { return ch.connectTo(1) }

// connectTo opens a game client connection to channel n.
func (ch *chain) connectTo(n int) *client {
	ch.t.Helper()
	nc, err := net.Dial("tcp", ch.addrs[n-1])
	if err != nil {
		ch.t.Fatal(err)
	}
	ch.t.Cleanup(func() { nc.Close() })
	cl := &client{t: ch.t, nc: nc, c: protocol.NewDialedConn(nc)}
	if err := cl.c.Hello(); err != nil {
		ch.t.Fatal(err)
	}
	return cl
}

func (cl *client) send(v any) {
	cl.t.Helper()
	if err := cl.c.SendRaw(wire.MustMarshal(v)); err != nil {
		cl.t.Fatal(err)
	}
}

// waitFor reads packets until one with id op arrives (others are skipped).
func (cl *client) waitFor(op uint16, v any) protocol.Packet {
	cl.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var seen []string
	for {
		_ = cl.nc.SetReadDeadline(deadline)
		pkt, err := cl.c.ReadPacket()
		if err != nil {
			cl.t.Fatalf("waiting for 0x%X: %v (saw %v)", op, err, seen)
		}
		if pkt.Header.PacketID == op {
			if v != nil {
				if err := wire.Unmarshal(pkt.Raw, v); err != nil {
					cl.t.Fatal(err)
				}
			}
			return pkt
		}
		seen = append(seen, fmt.Sprintf("0x%X", pkt.Header.PacketID))
	}
}

func (cl *client) login(user, pw, numeric string) wire.CNFAccountLogin {
	cl.t.Helper()
	p := wire.AccountLogin{Header: wire.NewHeader(wire.OpCliLogin, 0)}
	wire.PutCString(p.Login[:], user)
	wire.PutCString(p.Password[:], pw)
	p.Mac = [8]byte{1, 2, 3, byte(len(user))}
	cl.send(&p)
	var list wire.CNFAccountLogin
	pkt := cl.waitFor(wire.OpSrvCharList, &list)
	if pkt.Header.ClientID != wire.ClientIDLogin {
		cl.t.Errorf("char list client id 0x%X", pkt.Header.ClientID)
	}

	// Entering without the numeric password is refused.
	cl.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCliEnterWorld, 0)})
	cl.waitFor(wire.OpNumericPasswordWrong, nil)

	np := wire.NumericPassword{Header: wire.NewHeader(wire.OpCliNumericPass, 0)}
	wire.PutCString(np.Num[:], numeric)
	cl.send(&np)
	cl.waitFor(wire.OpNumericPassword, nil)
	return list
}

// enter goes to the world and returns the 0x114 plus every spawn packet
// received before 0x337 (own spawn first, then others via SendGridMob).
func (cl *client) enter() (wire.CNFCharacterLogin, []wire.CreateMob) {
	cl.t.Helper()
	cl.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCliEnterWorld, 0), CharIndex: 0})
	var w wire.CNFCharacterLogin
	cl.waitFor(wire.OpSrvEnterWorld, &w)
	var spawns []wire.CreateMob
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = cl.nc.SetReadDeadline(deadline)
		pkt, err := cl.c.ReadPacket()
		if err != nil {
			cl.t.Fatalf("entering world: %v", err)
		}
		switch pkt.Header.PacketID {
		case wire.OpSrvCreateMob:
			var m wire.CreateMob
			_ = wire.Unmarshal(pkt.Raw, &m)
			spawns = append(spawns, m)
		case wire.OpSrvEtc:
			return w, spawns
		}
	}
}

func TestPlayFlow(t *testing.T) {
	ch := newChain(t)
	ctx := context.Background()

	a := ch.connect()
	listA := a.login("playera", "pwa", "1234")
	if wire.CString(listA.CharList.Name[0][:]) != "Alpha" || wire.CString(listA.UserName[:]) != "PLAYERA" {
		t.Fatalf("char list: %q / %q", listA.CharList.Name[0][:], listA.UserName[:])
	}
	if listA.Keys == [16]byte{} {
		t.Error("char list must carry 16 session keys")
	}
	wa, spawnsA := a.enter()
	if wire.CString(wa.Mob.Name[:]) != "Alpha" || wa.ClientIndex <= 0 {
		t.Fatalf("enter world: %q index %d", wa.Mob.Name[:], wa.ClientIndex)
	}
	if len(spawnsA) != 1 || spawnsA[0].Index != wa.ClientIndex || spawnsA[0].ItemRefine[0] == 0 {
		t.Errorf("own spawn: %d packets", len(spawnsA))
	}
	t.Logf("Alpha entered at %dx%d", wa.WorldPos.X, wa.WorldPos.Y)

	b := ch.connect()
	b.login("PLAYERB", "pwb", "4321")
	wb, spawnsB := b.enter()

	// Both low-level mortals spawn in the training area: each sees the other.
	sawAlpha := false
	for _, s := range spawnsB {
		if s.Index == wa.ClientIndex && wire.CString(s.Name[:]) == "Alpha" {
			sawAlpha = true
		}
	}
	if !sawAlpha {
		t.Errorf("Bravo did not get Alpha's spawn (%d spawns)", len(spawnsB))
	}
	var seen wire.CreateMob
	a.waitFor(wire.OpSrvCreateMob, &seen) // Bravo's spawn broadcast
	if seen.Index != wb.ClientIndex || wire.CString(seen.Name[:]) != "Bravo" {
		t.Errorf("Alpha saw %d %q", seen.Index, seen.Name[:])
	}

	// Walk: Bravo sees Alpha's movement.
	dest := wire.Position{X: wa.WorldPos.X + 3, Y: wa.WorldPos.Y}
	a.send(&wire.Action{Header: wire.NewHeader(wire.OpCliMove, 0), LastPos: wa.WorldPos, MoveSpeed: 2, Destiny: dest})
	var mv wire.Action
	pkt := b.waitFor(wire.OpCliMove, &mv)
	if int(pkt.Header.ClientID) != int(wa.ClientIndex) || mv.Destiny.X == 0 {
		t.Errorf("relayed move from %d to %+v", pkt.Header.ClientID, mv.Destiny)
	}

	// Chat.
	chat := wire.Chat{Header: wire.NewHeader(wire.OpCliChat, 0)}
	wire.PutCString(chat.Text[:], "ola mundo")
	a.send(&chat)
	var heard wire.Chat
	b.waitFor(wire.OpCliChat, &heard)
	if wire.CString(heard.Text[:]) != "ola mundo" {
		t.Errorf("chat %q", heard.Text[:])
	}

	// Trade: Bravo opens, Alpha answers, Bravo offers 100 gold, both confirm.
	offer := func(to int16, gold int64, confirm uint8) *wire.Trade {
		tr := &wire.Trade{Header: wire.NewHeader(wire.OpCliTrade, 0), ClientID: uint16(to), Gold: gold, Confirm: confirm}
		for i := range tr.Slot {
			tr.Slot[i] = -1
		}
		return tr
	}
	var got wire.Trade
	b.send(offer(wa.ClientIndex, 0, 0))
	a.waitFor(wire.OpCliTrade, &got)
	if int16(got.ClientID) != wb.ClientIndex {
		t.Fatalf("trade request from %d, want %d", got.ClientID, wb.ClientIndex)
	}
	a.send(offer(wb.ClientIndex, 0, 0))
	b.waitFor(wire.OpCliTrade, nil)
	b.send(offer(wa.ClientIndex, 100, 0))
	a.waitFor(wire.OpCliTrade, &got)
	if got.Gold != 100 {
		t.Errorf("relayed offer gold %d", got.Gold)
	}
	b.send(offer(wa.ClientIndex, 100, 1))
	b.waitFor(wire.OpSrvTradeConfirm, nil)
	a.send(offer(wb.ClientIndex, 0, 1))
	var carryA, carryB wire.Carry
	a.waitFor(wire.OpSrvCarry, &carryA)
	b.waitFor(wire.OpSrvCarry, &carryB)
	if carryA.Gold != 600 || carryB.Gold != 400 || carryA.Item[0].Index != 1103 {
		t.Errorf("after trade: Alpha gold %d item0 %d, Bravo gold %d", carryA.Gold, carryA.Item[0].Index, carryB.Gold)
	}

	// Move an item: inventory 0 -> 5.
	a.send(&wire.MoveItem{Header: wire.NewHeader(wire.OpCliMoveItem, 0), SrcType: wire.SlotInv, SrcSlot: 0, DstType: wire.SlotInv, DstSlot: 5})
	a.waitFor(wire.OpCliMoveItem, nil)

	// Back to the character list: TMSRV saves (0x807) first.
	a.send(&wire.Signal{Header: wire.NewHeader(wire.OpCliLogout, 0)})
	a.waitFor(wire.OpSrvBackToList, nil)
	b.waitFor(wire.OpSrvRemoveMob, nil) // Alpha left Bravo's view

	waitUntil(t, func() bool {
		rec, err := ch.st.LoadAccount(ctx, "PLAYERA")
		return err == nil && rec.Account.Mob[0].Player.Inventory[5].Index == 1103 && rec.Account.Mob[0].Player.Inventory[0].Index == 0 && rec.Account.Mob[0].Player.Gold == 600
	})

	// Gold to storage (0x388) and part of it back (0x387); saved right away.
	b.send(&wire.SignalValue64{Header: wire.NewHeader(wire.OpCliGoldToStorage, 0), Value: 300})
	var cargo wire.CargoCoin
	b.waitFor(wire.OpSrvCargoCoin, &cargo)
	if cargo.Gold != 300 {
		t.Errorf("storage gold %d, want 300", cargo.Gold)
	}
	b.send(&wire.SignalValue64{Header: wire.NewHeader(wire.OpCliGoldToInv, 0), Value: 100})
	b.waitFor(wire.OpSrvCargoCoin, &cargo)
	if cargo.Gold != 200 {
		t.Errorf("storage gold %d, want 200", cargo.Gold)
	}
	b.send(&wire.SignalValue64{Header: wire.NewHeader(wire.OpCliGoldToInv, 0), Value: 5000}) // more than stored
	b.waitFor(wire.OpSrvMessage, nil)
	waitUntil(t, func() bool {
		rec, err := ch.st.LoadAccount(ctx, "PLAYERB")
		return err == nil && rec.Account.StorageCoin == 200 && rec.Account.Mob[0].Player.Gold == 200 // 500 - 100 traded - 300 + 100
	})

	// Disconnecting while playing saves and quits (0x806) and frees the account.
	b.nc.Close()
	waitUntil(t, func() bool { on, _ := ch.st.Online(ctx, "PLAYERB"); return on == nil })
}

// TestSealFlow seals an Arch with the Selo da Alma (3443), gets the filled
// seal in storage, then opens it from another character of the account.
func TestSealFlow(t *testing.T) {
	ch := newChain(t)
	ctx := context.Background()

	// PLAYERA: slot 0 is a bare Arch with an empty seal, slot 1 a level 300
	// Arch that will open it.
	rec, err := ch.st.LoadAccount(ctx, "PLAYERA")
	if err != nil {
		t.Fatal(err)
	}
	arch := func(m *wire.CharInfo, name string) {
		face := m.Player.Equip[0]
		for i := 1; i < len(m.Player.Equip); i++ {
			m.Player.Equip[i] = wire.Item{}
		}
		m.Player.Inventory = [64]wire.Item{}
		m.Player.Equip[0] = face
		m.Player.Equip[0].Effect[1].Value = 2 // Arch
		m.Player.BStatus.Level, m.Player.Status.Level = 300, 300
		m.Player.Gold = 0
		wire.PutCString(m.Player.Name[:], name)
	}
	arch(&rec.Account.Mob[0], "Alpha")
	rec.Account.Mob[0].Player.Inventory[0] = wire.Item{Index: 3443}
	rec.Account.Mob[1] = rec.Account.Mob[0]
	arch(&rec.Account.Mob[1], "Opener")
	if err := ch.st.SaveAccount(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if ok, err := ch.st.ReserveName(ctx, rec.ID, "Opener", false); !ok || err != nil {
		t.Fatal("reserve Opener", err)
	}

	a := ch.connect()
	a.login("playera", "pwa", "1234")
	a.enter()
	a.send(&wire.UseItem{Header: wire.NewHeader(wire.OpCliUseItem, 0), SrcType: wire.SlotInv, SrcSlot: 0})
	a.waitFor(wire.OpSrvBackToList, nil)
	var list wire.CharListPacket
	a.waitFor(wire.OpSrvCharListDel, &list)
	if list.CharList.Name[0][0] != 0 {
		t.Fatalf("sealed character still listed: %q", list.CharList.Name[0][:])
	}
	var seal wire.Item
	waitUntil(t, func() bool {
		r, err := ch.st.LoadAccount(ctx, "PLAYERA")
		if err != nil || r.Account.Mob[0].Player.Name[0] != 0 {
			return false
		}
		for _, it := range r.Account.StorageItems {
			if it.Index == 3443 && it.Effect[0].Index != 0 {
				seal = it
				return true
			}
		}
		return false
	})

	// Opener takes the seal out of storage and opens it as "Novo".
	a.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCliEnterWorld, 0), CharIndex: 1})
	a.waitFor(wire.OpSrvEnterWorld, nil)
	a.waitFor(wire.OpSrvEtc, nil)
	r, _ := ch.st.LoadAccount(ctx, "PLAYERA")
	from := -1
	for i, it := range r.Account.StorageItems {
		if it == seal {
			from = i
		}
	}
	a.send(&wire.MoveItem{Header: wire.NewHeader(wire.OpCliMoveItem, 0), SrcType: wire.SlotStorage, SrcSlot: uint8(from), DstType: wire.SlotInv, DstSlot: 3})
	a.waitFor(wire.OpCliMoveItem, nil)
	out := wire.PutOutSeal{Header: wire.NewHeader(0x3CC, 0), SrcType: wire.SlotInv, SrcSlot: 3}
	wire.PutCString(out.MobName[:], "Novo")
	a.send(&out)
	var slot wire.SignalValue
	a.waitFor(0x3B4, &slot)
	a.waitFor(wire.OpSrvCharListNew, &list)
	if wire.CString(list.CharList.Name[slot.Value][:]) != "Novo" {
		t.Fatalf("slot %d: %q", slot.Value, list.CharList.Name[slot.Value][:])
	}
	waitUntil(t, func() bool {
		r, err := ch.st.LoadAccount(ctx, "PLAYERA")
		return err == nil && wire.CString(r.Account.Mob[slot.Value].Player.Name[:]) == "Novo" &&
			r.Account.Mob[1].Player.Inventory[3].Index == 0
	})
}

// TestGuildCreatePersists: "/criar" reaches Postgres through DBSRV, where
// the next TMSRV start reads it (SetGuilds).
func TestGuildCreatePersists(t *testing.T) {
	ch := newChain(t)
	ctx := context.Background()
	rec, err := ch.st.LoadAccount(ctx, "PLAYERA")
	if err != nil {
		t.Fatal(err)
	}
	rec.Account.Mob[0].Player.Gold = 150000000
	if err := ch.st.SaveAccount(ctx, rec); err != nil {
		t.Fatal(err)
	}
	a := ch.connect()
	a.login("playera", "pwa", "1234")
	a.enter()
	cmd := wire.Command{Header: wire.NewHeader(wire.OpCliCommand, 0)}
	wire.PutCString(cmd.Command[:], "criar")
	wire.PutCString(cmd.Value[:], "Lobos")
	a.send(&cmd)
	a.waitFor(wire.OpSrvGold, nil)
	waitUntil(t, func() bool {
		gs, err := ch.st.Guilds(ctx)
		return err == nil && len(gs) == 1 && gs[0].Name == "Lobos" && gs[0].ID == 1
	})
	// Logout saves the leader with the guild.
	a.send(&wire.Signal{Header: wire.NewHeader(wire.OpCliLogout, 0)})
	a.waitFor(wire.OpSrvBackToList, nil)
	waitUntil(t, func() bool {
		r, err := ch.st.LoadAccount(ctx, "PLAYERA")
		p := r.Account.Mob[0].Player
		return err == nil && p.GuildIndex == 1 && p.GuildMemberType == 9 && p.Gold == 50000000
	})
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
