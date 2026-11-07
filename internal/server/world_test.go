package server

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// Phase 4 end-to-end tests. World packets are sent with client id 0 so they
// share one lane and replies arrive in order.

func TestGuilds(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch1, ch2 := h.connect(), h.connect()
	both := func(op uint16, v any) {
		t.Helper()
		ch1.expect(op, v)
		ch2.expect(op, v)
	}

	cg := wire.CreateGuild{Header: wire.NewHeader(wire.OpCreateGuild, 0), Kingdom: 8, Citizen: 1, GuildID: 9}
	wire.PutCString(cg.GuildName[:], "NEWGUILD")
	ch1.send(&cg)
	var gotCG wire.CreateGuild
	both(wire.OpCreateGuild, &gotCG)
	if wire.CString(gotCG.GuildName[:]) != "NEWGUILD" || gotCG.GuildID != 9 {
		t.Fatalf("create guild broadcast %+v", gotCG)
	}

	// Creating over an existing guild is refused silently.
	dup := cg
	dup.GuildID = 6
	ch1.send(&dup)

	ch1.send(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: 500, GuildIndex: 9})
	both(wire.OpAddGuild, nil) // the dup produced nothing before this

	sub := wire.AddSub{Header: wire.NewHeader(wire.OpAddSub, 0), GuildIndex: 9, SubIndex: 1}
	wire.PutCString(sub.Name[:], "SUBX")
	ch1.send(&sub)
	both(wire.OpAddSub, nil)

	ch1.send(&wire.AddSub{Header: wire.NewHeader(wire.OpAddSub, 0), GuildIndex: -5, SubIndex: 1}) // C++ wrote out of bounds
	ch1.send(&wire.GuildRelation{Header: wire.NewHeader(wire.OpGuildAlly, 0), GuildIndex1: 9, GuildIndex2: 6})
	var rel wire.Parm3
	both(wire.OpGuildAlly, &rel)
	if rel.Parm1 != 9 || rel.Parm2 != 6 {
		t.Errorf("ally broadcast %+v", rel)
	}

	ch1.send(&wire.GuildZoneReport{Header: wire.NewHeader(wire.OpGuildZoneReport, 1), Guild: [5]int32{9, 6}})
	var charged wire.ChargedGuildList
	ch1.expect(wire.OpCNFChargedList, &charged)
	ch2.expect(wire.OpCNFChargedList, nil)
	if charged.ChargedGuildList[1] != [5]int32{9, 6} {
		t.Errorf("charged list %v", charged.ChargedGuildList[1])
	}

	ch1.send(&wire.Signal{Header: wire.NewHeader(wire.OpPanelGuildList, 0)})
	var panel wire.PanelGuildList
	ch1.expect(wire.OpPanelGuildList, &panel)
	if panel.Guild[6].Name.String() != "OvErLoRd" || panel.Guild[9].Fame != 500 || panel.Guild[9].SubGuild[1].String() != "SUBX" {
		t.Errorf("panel: 6=%q 9=%q fame %d sub %q", panel.Guild[6].Name.String(), panel.Guild[9].Name.String(),
			panel.Guild[9].Fame, panel.Guild[9].SubGuild[1].String())
	}

	guilds, _ := h.st.Guilds(ctx)
	var g9 *store.Guild
	for i := range guilds {
		if guilds[i].ID == 9 {
			g9 = &guilds[i]
		}
	}
	if g9 == nil || g9.Name != "NEWGUILD" || g9.Fame != 500 || g9.SubGuild[1] != "SUBX" || g9.Kingdom != 8 {
		t.Errorf("guild 9 in db: %+v", g9)
	}
	ally, _ := h.st.GuildRelations(ctx, store.RelationAlly)
	if ally[9] != 6 {
		t.Errorf("ally relation not persisted: %v", ally)
	}
}

func TestDonateStoreAndBroadcasts(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch1, ch2 := h.connect(), h.connect()
	avail := func() int {
		slots, _ := h.st.DonateStore(ctx)
		return slots[0].Available
	}

	buy := wire.StoreSlot{Header: wire.NewHeader(wire.OpBuyStoreSaveList, 0), Store: 1, Slot: 2}
	for i, want := range []int{1, 0, 0} { // never wraps to -1 (unlimited)
		ch1.send(&buy)
		var got wire.StoreSlot
		ch1.expect(wire.OpBroadcastStoreBuy, &got)
		ch2.expect(wire.OpBroadcastStoreBuy, nil)
		if got.Store != 1 || got.Slot != 2 || avail() != want {
			t.Fatalf("buy %d: %+v available %d, want %d", i, got, avail(), want)
		}
	}

	ch1.send(&wire.StoreSetAvaible{Header: wire.NewHeader(wire.OpBroadcastSetAvaible, 0), Store: 50, Slot: 2, Avaible: 9}) // out of range: ignored
	ch1.send(&wire.StoreSetAvaible{Header: wire.NewHeader(wire.OpBroadcastSetAvaible, 0), Store: 1, Slot: 2, Avaible: 5})
	ch1.expect(wire.OpBroadcastSetAvaible, nil)
	ch2.expect(wire.OpBroadcastSetAvaible, nil)
	if avail() != 5 {
		t.Errorf("available %d", avail())
	}

	// Opaque relays keep their body.
	chat := wire.Notice96{Header: wire.NewHeader(wire.OpBroadcastChat, 0)}
	wire.PutCString(chat.Text[:], "hello channels")
	ch2.send(&chat)
	for _, c := range []*tmsrv{ch1, ch2} {
		pkt := c.expect(wire.OpBroadcastChat, nil)
		if !bytes.Contains(pkt.Raw, []byte("hello channels")) {
			t.Error("chat body lost")
		}
	}

	notice := wire.Notice96{Header: wire.NewHeader(wire.OpServerNotice, 0)}
	wire.PutCString(notice.Text[:], "manutencao")
	ch1.send(&notice)
	if p := ch1.expect(wire.OpServerNotice, nil); p.Header.ClientID != 0 {
		t.Errorf("notice to channel 0 has client id %d", p.Header.ClientID)
	}
	if p := ch2.expect(wire.OpServerNotice, nil); p.Header.ClientID != 1 {
		t.Errorf("notice to channel 1 has client id %d", p.Header.ClientID)
	}

	ch1.send(&wire.Parm1{Header: wire.NewHeader(wire.OpUpdateSapphire, 0), Parm1: 1})
	var sp wire.Parm3
	ch1.expect(wire.OpUpdateSapphire, &sp)
	ch2.expect(wire.OpUpdateSapphire, nil)
	if v, _ := h.st.Sapphire(ctx); v != 16 || sp.Parm1 != 1 {
		t.Errorf("sapphire %d (parm %d)", v, sp.Parm1)
	}
}

func TestKefra(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	ch1, ch2 := h.connect(), h.connect()

	kill := wire.KefraDeath{Header: wire.NewHeader(wire.OpNotifyKefraDeath, 0)}
	wire.PutCString(kill.Name[:], "Hero")
	ch2.send(&kill)
	var first wire.FirstKefra
	ch1.expect(wire.OpFirstKefraNotify, &first)
	ch2.expect(wire.OpFirstKefraNotify, nil)
	if first.Channel != 1 {
		t.Errorf("first kefra channel %d", first.Channel)
	}
	if k, _ := h.st.Kefra(ctx); k.FirstChannel != 1 || k.Killer != "Hero" {
		t.Errorf("kefra state %+v", k)
	}

	h.srv.resetKefra(ctx) // the Wednesday 19:00 job
	ch1.expect(wire.OpFirstKefraNotify, &first)
	if first.Channel != maxServerGroup {
		t.Errorf("reset channel %d", first.Channel)
	}
	ch1.expect(wire.OpRebornKefra, nil)
	ch2.expect(wire.OpFirstKefraNotify, nil)
	ch2.expect(wire.OpRebornKefra, nil)
	if k, _ := h.st.Kefra(ctx); k.FirstChannel != maxServerGroup || k.Killer != "" {
		t.Errorf("kefra after reset %+v", k)
	}
}

func TestSeals(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	const c = 4
	tm.loginOK(c, "TESTER", "secret")
	tm.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, c), CharIndex: 0})
	tm.expect(wire.OpCNFCharacterLoginExtra, nil)
	tm.expect(wire.OpCNFCharacterLogin, nil)

	tm.send(&wire.SignalValue{Header: wire.NewHeader(wire.OpSealInfo, c), Value: 999})
	var info wire.SealInfoReply
	tm.expect(wire.OpSealInfo, &info)
	if info.Info.Status != -1 {
		t.Errorf("missing seal status %d", info.Info.Status)
	}

	// Seal "Hero" (slot 0).
	tm.send(&wire.SignalValue{Header: wire.NewHeader(wire.OpPutInSeal, c)})
	var cl wire.CharListPacket
	tm.expect(wire.OpCNFDeleteCharacter, &cl)
	if cl.CharList.Name[0][0] != 0 {
		t.Fatal("sealed character still listed")
	}
	var ok wire.SignalValue
	tm.expect(wire.OpPutInSealSuccess, &ok)
	var item wire.ItemDelivery
	tm.expect(wire.OpItemDelivery, &item)
	sealID := int(item.Item.Effect[0].Value)*256 + int(item.Item.Effect[1].Value)
	if item.Item.Index != sealItemIndex || item.Item.Effect[0].Index != sealItemEffect || sealID < 1 {
		t.Fatalf("seal item %+v", item.Item)
	}
	if _, err := h.st.NameOwner(ctx, "hero"); err != store.ErrNotFound {
		t.Error("sealed character's name should be free")
	}

	tm.send(&wire.SignalValue{Header: wire.NewHeader(wire.OpSealInfo, c), Value: int32(sealID)})
	tm.expect(wire.OpSealInfo, &info)
	// Face is Equip[0].EF2 (C++); the seeded raw TK template has EF2 = 98,
	// characters made by 0x802 get EF2 = their face.
	if info.Info.Status != int32(sealID) || info.Info.Face != 98 || info.Info.STR != 8 || info.Info.Evolution != 1 {
		t.Errorf("seal info %+v", info.Info)
	}

	// Make a holder character, give it the seal item via a save, unseal.
	nc := wire.NewCharacter{Header: wire.NewHeader(wire.OpNewCharacter, c), SlotID: 1, ClassID: 0}
	wire.PutCString(nc.Nick[:], "Holder")
	tm.send(&nc)
	tm.expect(wire.OpCNFNewCharacter, nil)
	tm.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, c), CharIndex: 1})
	var extra wire.CNFCharacterLoginExtra
	tm.expect(wire.OpCNFCharacterLoginExtra, &extra)
	tm.expect(wire.OpCNFCharacterLogin, nil)
	extra.Mob.Player.Inventory[5] = item.Item
	tm.save(wire.OpSaveMob, c, "TESTER", 1, extra.Mob)

	out := wire.PutOutSeal{Header: wire.NewHeader(wire.OpOutSeal, c), SrcSlot: 5}
	wire.PutCString(out.MobName[:], "Reborn")
	tm.send(&out)
	tm.expect(wire.OpCNFNewCharacter, &cl)
	if wire.CString(cl.CharList.Name[0][:]) != "Reborn" || cl.CharList.Equip[0][0].Index != 1 {
		t.Fatalf("unsealed into slot 0: %q face %d", cl.CharList.Name[0][:], cl.CharList.Equip[0][0].Index)
	}
	if owner, _ := h.st.NameOwner(ctx, "reborn"); owner != "TESTER" {
		t.Error("unsealed name not reserved")
	}

	out.SrcSlot = 500 // C++ indexed this unchecked
	tm.send(&out)
	tm.send(&wire.SignalValue{Header: wire.NewHeader(wire.OpSealInfo, c), Value: 999})
	tm.expect(wire.OpSealInfo, nil) // nothing came back for the bad slot
}

func TestDeliveries(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
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
	pending := func(id int64, want string) {
		t.Helper()
		waitFor(t, func() bool {
			done, r, _ := h.st.DeliveryResult(ctx, id)
			return !done && r == want
		})
	}
	enqueue := func(kind, user string, payload any) int64 {
		t.Helper()
		id, err := h.st.Enqueue(ctx, kind, user, payload, "test")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	waitFor(t, func() bool { // written by the jobs goroutine at startup
		proto, seen, _, _ := h.st.DBSRVStatus(ctx)
		return proto == store.DeliveryProtocol && seen != nil
	})

	// Offline account (OTHER).
	if r := wait(enqueue("cash", "other", map[string]any{"amount": 250})); r != resOK {
		t.Errorf("cash: %s", r)
	}
	if r := wait(enqueue("item", "OTHER", map[string]any{"item_id": 4140, "effects": [][2]int{{61, 10}}})); !strings.HasPrefix(r, resOK) {
		t.Errorf("item: %s", r)
	}
	rec, _ := h.st.LoadAccount(ctx, "OTHER")
	if rec.Account.Cash != 250 || rec.Account.StorageItems[0].Index != 4140 || rec.Account.StorageItems[0].Effect[0].Value != 10 {
		t.Errorf("offline delivery: cash %d storage[0] %+v", rec.Account.Cash, rec.Account.StorageItems[0])
	}
	if r := wait(enqueue("password", "OTHER", map[string]any{"password": "novasenha", "numeric": "4321"})); r != resOK {
		t.Errorf("password: %s", r)
	}
	if r := wait(enqueue("item", "NOBODY", map[string]any{"item_id": 1})); r != resNotFound {
		t.Errorf("unknown account: %s", r)
	}
	if r := wait(enqueue("create_account", "newbie", map[string]any{"password": "abc123"})); r != resOK {
		t.Errorf("create: %s", r)
	}
	if r := wait(enqueue("create_account", "NEWBIE", map[string]any{"password": "x"})); r != resExists {
		t.Errorf("create twice: %s", r)
	}
	if r := wait(enqueue("cash", "OTHER", map[string]any{"amount": -5})); !strings.HasPrefix(r, resInvalid) {
		t.Errorf("negative cash: %s", r)
	}

	// The admin panel sends hashes only; a token-only change keeps the password.
	pwHash, _ := auth.Hash("viahash")
	numHash, _ := auth.Hash("777")
	if r := wait(enqueue("create_account", "HASHED", map[string]any{"password_hash": pwHash, "numeric_hash": numHash})); r != resOK {
		t.Errorf("create with hashes: %s", r)
	}
	tokOnly, _ := auth.Hash("2468")
	if r := wait(enqueue("password", "NEWBIE", map[string]any{"numeric_hash": tokOnly})); r != resOK {
		t.Errorf("token only: %s", r)
	}
	if r := wait(enqueue("password", "NEWBIE", map[string]any{"password_hash": "plaintext"})); !strings.HasPrefix(r, resInvalid) {
		t.Errorf("non-argon hash accepted: %s", r)
	}
	hashed, _ := h.st.LoadAccount(ctx, "HASHED")
	if ok, _ := auth.Verify(hashed.NumericPassHash, "777"); !ok {
		t.Error("numeric hash from create_account not stored")
	}
	if d, _ := h.st.RecentDeliveries(ctx, "HASHED", 1); len(d) == 1 {
		var left []byte
		_ = h.st.Pool().QueryRow(ctx, `SELECT payload::text FROM deliveries WHERE id = $1`, d[0].ID).Scan(&left)
		if strings.Contains(string(left), "argon2id") {
			t.Errorf("secrets kept in processed delivery: %s", left)
		}
	}

	tm := h.connect()
	tm.loginOK(1, "OTHER", "novasenha")
	tm.loginOK(2, "NEWBIE", "abc123") // password unchanged by the token-only request
	tm.loginOK(4, "HASHED", "viahash")
	newbie, _ := h.st.LoadAccount(ctx, "NEWBIE")
	if ok, _ := auth.Verify(newbie.NumericPassHash, "2468"); !ok {
		t.Error("token-only change not applied")
	}
	rec, _ = h.st.LoadAccount(ctx, "OTHER")
	if ok, _ := auth.Verify(rec.NumericPassHash, "4321"); !ok {
		t.Error("numeric password not set")
	}

	// Online at character selection: items wait, cash goes to TMSRV.
	tm.loginOK(3, "TESTER", "secret")
	itemID := enqueue("item", "TESTER", map[string]any{"item_id": 777})
	pending(itemID, waitCharSelected)
	cashID := enqueue("cash", "TESTER", map[string]any{"amount": 99})
	var cash wire.CashDelivery
	p := tm.expect(wire.OpCashDelivery, &cash)
	if cash.Cash != 99 || wire.CString(cash.Username[:]) != "TESTER" || p.Header.ClientID != 3 {
		t.Errorf("cash online %+v client %d", cash, p.Header.ClientID)
	}
	if r := wait(cashID); r != resSentOnline {
		t.Errorf("cash online result %s", r)
	}
	pwID := enqueue("password", "TESTER", map[string]any{"password": "zzz"})
	pending(pwID, waitOffline)

	// Entering the game releases the item to TMSRV.
	tm.send(&wire.CharacterLogin{Header: wire.NewHeader(wire.OpCharacterLogin, 3), CharIndex: 0})
	tm.expect(wire.OpCNFCharacterLoginExtra, nil)
	tm.expect(wire.OpCNFCharacterLogin, nil)
	var it wire.ItemDelivery
	tm.expect(wire.OpItemDelivery, &it)
	if it.Item.Index != 777 {
		t.Errorf("item online %+v", it.Item)
	}
	if r := wait(itemID); r != resSentOnline {
		t.Errorf("item online result %s", r)
	}

	// Ban while online: DB updated and TMSRV told.
	until := time.Now().Add(72 * time.Hour)
	banID := enqueue("ban", "TESTER", map[string]any{"type": 1, "until": until})
	var ban wire.BanNotify
	tm.expect(wire.OpBanNotify, &ban)
	if ban.BanType != 1 || int(ban.Ban.Ano) != until.In(testLoc).Year() {
		t.Errorf("ban notify %+v", ban)
	}
	if r := wait(banID); r != resSentOnline {
		t.Errorf("ban result %s", r)
	}

	// Logout lets the password change through.
	tm.send(&wire.Signal{Header: wire.NewHeader(wire.OpNoNeedSave, 3)})
	if r := wait(pwID); r != resOK {
		t.Errorf("deferred password: %s", r)
	}
	tm.login(3, "TESTER", "zzz", false)
	tm.expect(wire.OpAccountBlocked, nil) // and the ban holds
}
