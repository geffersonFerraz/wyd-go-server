package store

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"dbsrv/internal/wire"
)

// testServer opens the world of another server on the database testStore
// prepared.
func testServer(t *testing.T, server int) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := OpenServer(ctx, os.Getenv("DBSRV_TEST_DSN"), testLoc, server)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil { // idempotent
		t.Fatal(err)
	}
	return s
}

// Server 1 got its tables from the old migrations and a new server gets them
// from migrations/world: both must end up with the same layout.
func TestWorldSchemasMatch(t *testing.T) {
	s1 := testStore(t)
	testServer(t, 2)
	ctx := context.Background()
	for what, q := range map[string]string{
		"columns": `SELECT table_name || '.' || column_name || ' ' || data_type || ' ' || is_nullable || ' ' ||
				COALESCE(replace(column_default, $1 || '.', ''), '')
			FROM information_schema.columns WHERE table_schema = $1 ORDER BY 1`,
		"indexes": `SELECT replace(indexdef, $1 || '.', '') FROM pg_indexes WHERE schemaname = $1 ORDER BY 1`,
		"constraints": `SELECT c.conrelid::regclass::text || ' ' || c.conname || ' ' || pg_get_constraintdef(c.oid)
			FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace WHERE n.nspname = $1 ORDER BY 1`,
		"triggers": `SELECT replace(pg_get_triggerdef(t.oid), $1 || '.', '')
			FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND NOT t.tgisinternal ORDER BY 1`,
		"sequences": `SELECT sequencename || ' ' || start_value || ' ' || increment_by FROM pg_sequences WHERE schemaname = $1 ORDER BY 1`,
	} {
		list := func(schema string) []string {
			// regclass prints unqualified names for the schema first in the path.
			rows, err := s1.pool.Query(ctx, q, schema)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var out []string
			for rows.Next() {
				var v string
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				out = append(out, strings.ReplaceAll(v, schema+".", ""))
			}
			return out
		}
		a, b := list("s1"), list("s2")
		if len(a) == 0 {
			t.Errorf("%s: nothing in s1", what)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s differ between s1 and s2:\ns1: %s\ns2: %s", what, strings.Join(a, "\n    "), strings.Join(b, "\n    "))
		}
	}
	// Nothing of a world may be left where every server would see it.
	var n int
	if err := s1.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables a JOIN information_schema.tables b
		ON b.table_name = a.table_name AND b.table_schema = 'public' WHERE a.table_schema = 's1'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d world tables also exist in public (err %v)", n, err)
	}
}

func TestServersShareAccountsNotWorlds(t *testing.T) {
	s1 := testStore(t)
	s2 := testServer(t, 2)
	ctx := context.Background()
	in := sampleAccount(t)
	in.Account.Cash, in.Account.StorageCoin = 500, 9000
	if err := s1.SaveAccount(ctx, in); err != nil {
		t.Fatal(err)
	}
	if ok, err := s1.ReserveName(ctx, in.ID, "Shared", false); err != nil || !ok {
		t.Fatal(ok, err)
	}

	// The same login on server 2: same account, another world.
	on2, err := s2.LoadAccount(ctx, "anderson")
	if err != nil {
		t.Fatal(err)
	}
	if on2.ID != in.ID || on2.PasswordHash != in.PasswordHash || on2.Account.Cash != 500 {
		t.Errorf("account not shared: id %d/%d cash %d", on2.ID, in.ID, on2.Account.Cash)
	}
	if on2.Account.StorageCoin != 0 || on2.Account.CharSlot != -1 || on2.Account.StorageItems != ([168]wire.Item{}) {
		t.Errorf("server 2 world not empty: gold %d slot %d", on2.Account.StorageCoin, on2.Account.CharSlot)
	}
	for i, m := range on2.Account.Mob {
		if wire.GameString(m.Player.Name[:]) != "" {
			t.Errorf("server 2 has character %d of server 1", i)
		}
	}
	// A name taken on server 1 is free on server 2.
	if ok, err := s2.ReserveName(ctx, on2.ID, "Shared", false); err != nil || !ok {
		t.Errorf("name of server 1 not free on server 2: %v %v", ok, err)
	}

	// Playing on server 2 spends cash and fills its storage.
	on2.Account.Cash, on2.Account.StorageCoin = 120, 77
	if err := s2.SaveAccount(ctx, on2); err != nil {
		t.Fatal(err)
	}
	back, err := s1.LoadAccount(ctx, "anderson")
	if err != nil {
		t.Fatal(err)
	}
	if back.Account.Cash != 120 {
		t.Errorf("cash is shared: server 1 sees %d, want 120", back.Account.Cash)
	}
	want := *in
	want.Account.Cash = 120
	if back.Account != want.Account {
		t.Error("the world of server 1 changed when server 2 saved the account")
	}
	if again, _ := s2.LoadAccount(ctx, "anderson"); again == nil || again.Account.StorageCoin != 77 {
		t.Errorf("server 2 storage gold not kept: %+v", again)
	}
}

func TestAccountIsHeldByOneServer(t *testing.T) {
	s1 := testStore(t)
	s2 := testServer(t, 2)
	ctx := context.Background()
	if err := s1.SaveAccount(ctx, sampleAccount(t)); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Store{s1, s2} {
		if err := s.Heartbeat(ctx, DeliveryProtocol); err != nil {
			t.Fatal(err)
		}
	}
	if ok, by, err := s1.ClaimOnline(ctx, "anderson", 0, 7); err != nil || !ok || by != 1 {
		t.Fatalf("claim on server 1: %v %d %v", ok, by, err)
	}
	if ok, _, err := s1.ClaimOnline(ctx, "anderson", 1, 9); err != nil || !ok { // channel change
		t.Fatalf("same server again: %v %v", ok, err)
	}
	if ok, by, err := s2.ClaimOnline(ctx, "anderson", 0, 3); err != nil || ok || by != 1 {
		t.Fatalf("server 2 took an account server 1 holds: %v %d %v", ok, by, err)
	}
	if ok, by, err := s2.ClaimOnline(ctx, "nobody", 0, 3); err != nil || ok || by != 0 {
		t.Fatalf("unknown account: %v %d %v", ok, by, err)
	}
	if on, _ := s2.Online(ctx, "anderson"); on == nil || on.Server != 1 || on.Channel != 1 || on.Client != 9 {
		t.Errorf("online seen from server 2: %+v", on)
	}
	// Server 2 cannot release or clear what server 1 holds.
	_ = s2.SetOffline(ctx, "anderson", 1, 9)
	_ = s2.ClearOnline(ctx, -1)
	if on, _ := s1.Online(ctx, "anderson"); on == nil {
		t.Fatal("server 2 released the account of server 1")
	}
	// A DBSRV that died stops holding its accounts.
	if _, err := s1.pool.Exec(ctx, `UPDATE server_status SET dbsrv_seen_at = now() - interval '5 minutes' WHERE server = 1`); err != nil {
		t.Fatal(err)
	}
	if ok, by, err := s2.ClaimOnline(ctx, "anderson", 0, 3); err != nil || !ok || by != 2 {
		t.Fatalf("server 2 could not take over from a dead server: %v %d %v", ok, by, err)
	}
	// A delivery's lease is not a player.
	_ = s2.SetOffline(ctx, "anderson", 0, 3)
	if ok, _, _ := s2.ClaimOnline(ctx, "anderson", -1, 0); !ok {
		t.Fatal("lease refused")
	}
	if on, _ := s2.Online(ctx, "anderson"); on != nil {
		t.Errorf("a lease shows as online: %+v", on)
	}
}

func TestDeliveriesBelongToAServer(t *testing.T) {
	s1 := testStore(t)
	s2 := testServer(t, 2)
	ctx := context.Background()
	id, err := s1.Enqueue(ctx, "cash", "anderson", map[string]any{"amount": 5}, "test")
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := s1.PendingDeliveries(ctx, 10)
	p2, _ := s2.PendingDeliveries(ctx, 10)
	if len(p1) != 1 || len(p2) != 0 {
		t.Fatalf("pending: server 1 %d, server 2 %d", len(p1), len(p2))
	}
	if err := s1.MoveDelivery(ctx, id, 2, "moved"); err != nil {
		t.Fatal(err)
	}
	p1, _ = s1.PendingDeliveries(ctx, 10)
	p2, _ = s2.PendingDeliveries(ctx, 10)
	if len(p1) != 0 || len(p2) != 1 || p2[0].ID != id {
		t.Fatalf("after the move: server 1 %d, server 2 %d", len(p1), len(p2))
	}
	if d, err := s1.DeliveryByID(ctx, id); err != nil || d.Server != 2 {
		t.Errorf("delivery %+v err %v", d, err)
	}
}

func TestOneDBSRVPerServer(t *testing.T) {
	s1 := testStore(t)
	s2 := testServer(t, 2)
	ctx := context.Background()
	release, err := s1.LockServer(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.LockServer(ctx, nil); err == nil || !strings.Contains(err.Error(), "already serving server 1") {
		t.Errorf("second DBSRV of server 1: %v", err)
	}
	release2, err := s2.LockServer(ctx, nil)
	if err != nil {
		t.Fatalf("server 2 blocked by server 1: %v", err)
	}
	release2()
	release()
	again, err := s1.LockServer(ctx, nil)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	again()
}

// A database from before the servers (one world in schema public, the whole
// account in the accounts table) becomes server 1 without losing anything.
func TestMigrationTurnsTheOldWorldIntoServer1(t *testing.T) {
	dsn := os.Getenv("DBSRV_TEST_DSN")
	if dsn == "" {
		t.Skip("DBSRV_TEST_DSN not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn, testLoc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.ResetForTests(ctx); err != nil {
		t.Fatal(err)
	}
	// The database as the previous version left it: migrations up to 0031.
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`)
	names, _ := migrationFS.ReadDir("migrations")
	applied := 0
	for _, e := range names {
		version := strings.TrimSuffix(e.Name(), ".sql")
		if e.IsDir() || version >= "0032" {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		exec(string(sql))
		exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		applied++
	}
	if applied != 31 {
		t.Fatalf("applied %d old migrations, want 31", applied)
	}

	// An account saved by the previous version (its saveAccountTx).
	in := sampleAccount(t)
	in.Account.Cash, in.Account.StorageCoin, in.Account.Novato, in.Account.AlreadyReset = 321, 123456789012, 1, true
	row, chars, items, err := splitAccount(in, testLoc)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO accounts (username, password_hash, status, numeric_pass_hash, pass_block, access_level,
		                      cash, honra, novato, storage_gold, ban_type, ban_until, already_reset, data)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
		row.Username, row.PasswordHash, row.Status, row.NumericPassHash, row.PassBlock, row.AccessLevel,
		row.Cash, row.Honra, row.Novato, row.StorageGold, row.BanType, row.BanUntil, row.AlreadyReset, row.Data).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, c := range chars {
		exec(`INSERT INTO characters (account_id, slot, name, class_info, face, evolution, level, exp, gold,
				guild_index, guild_member_type, fame, citizen, data) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			id, c.Slot, c.Name, c.ClassInfo, c.Face, c.Evolution, c.Level, c.Exp, c.Gold, c.GuildIndex, c.GuildMemberType, c.Fame, c.Citizen, c.Data)
	}
	for _, it := range items {
		e := it.Item.Effect
		exec(`INSERT INTO items (account_id, char_slot, container, slot, item_id, ef1, efv1, ef2, efv2, ef3, efv3)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, it.CharSlot, it.Container, it.Slot, int32(it.Item.Index),
			int16(e[0].Index), int32(e[0].Value), int16(e[1].Index), int32(e[1].Value), int16(e[2].Index), int32(e[2].Value))
	}
	exec(`INSERT INTO character_names (name_key, account_id) VALUES ('dragonblue', $1)`, id)
	exec(`INSERT INTO guilds (id, name) VALUES (6, 'OvErLoRd')`)
	exec(`INSERT INTO gamedata.city_owners (channel, city, guild_id) VALUES (2, 3, 6)
		ON CONFLICT (channel, city) DO UPDATE SET guild_id = 6`)
	exec(`UPDATE server_state SET sapphire = 14, dbsrv_protocol = 7, dbsrv_seen_at = now(), channel_online = '{3,-1}'`)
	exec(`INSERT INTO online_accounts (account_id, username, channel, client) VALUES ($1, $2, 1, 9)`, id, row.Username)
	exec(`INSERT INTO deliveries (kind, username) VALUES ('cash', $1)`, row.Username)
	exec(`SELECT nextval('seal_id_seq'), nextval('seal_id_seq')`)

	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	out, err := s.LoadAccount(ctx, row.Username)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != id || out.PasswordHash != in.PasswordHash || out.NumericPassHash != in.NumericPassHash || out.Status != in.Status {
		t.Errorf("account identity changed: %d %q", out.ID, out.PasswordHash)
	}
	if out.Account != in.Account {
		t.Errorf("account changed by the migration: cash %d storage gold %d novato %d reset %v",
			out.Account.Cash, out.Account.StorageCoin, out.Account.Novato, out.Account.AlreadyReset)
	}
	if g, err := s.Guilds(ctx); err != nil || len(g) != 1 || g[0].Name != "OvErLoRd" {
		t.Errorf("guilds %v err %v", g, err)
	}
	owners, err := s.CityOwners(ctx)
	found := false
	for _, o := range owners {
		found = found || (o.Channel == 2 && o.City == 3 && o.GuildID == 6)
	}
	if err != nil || !found {
		t.Errorf("city owner lost: %v err %v", owners, err)
	}
	if v, err := s.Sapphire(ctx); err != nil || v != 14 {
		t.Errorf("sapphire %d err %v", v, err)
	}
	if proto, seen, _, err := s.DBSRVStatus(ctx); err != nil || proto != 7 || seen == nil {
		t.Errorf("heartbeat lost: %d %v %v", proto, seen, err)
	}
	if online, _, err := s.ChannelStatus(ctx); err != nil || len(online) != 2 || online[0] != 3 {
		t.Errorf("channel counts lost: %v %v", online, err)
	}
	if on, err := s.Online(ctx, row.Username); err != nil || on == nil || on.Server != 1 || on.Channel != 1 {
		t.Errorf("online row: %+v err %v", on, err)
	}
	if p, err := s.PendingDeliveries(ctx, 10); err != nil || len(p) != 1 {
		t.Errorf("pending delivery not kept for server 1: %v err %v", p, err)
	}
	if n, err := s.NextSealID(ctx); err != nil || n != 3 {
		t.Errorf("seal sequence restarted: %d err %v", n, err)
	}
	if ok, err := s.ReserveName(ctx, id+1, "DragonBlue", false); err != nil || ok {
		t.Errorf("name reservation lost: %v %v", ok, err)
	}
	// The world tables left schema public.
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'
		AND table_name IN ('characters', 'items', 'guilds', 'ranking', 'server_state')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d world tables still in public (err %v)", n, err)
	}
}
