// Package store persists DBSRV state in PostgreSQL.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("store: not found")

// Store wraps a connection pool. Loc is the game's wall-clock time zone:
// STRUCT_ITEMDATE values (bans etc.) are local times without a zone.
//
// One database holds every server (world). The accounts, the game data and
// the delivery queue are shared (schema public / gamedata); the world of each
// server (characters, items, guilds, ranking...) is in its own schema
// (SchemaName). A Store works on one server: its connections have
// search_path = s<server>, public, so the unqualified queries of this
// package reach that world and no other.
type Store struct {
	pool   *pgxpool.Pool
	Loc    *time.Location
	server int
}

// MaxServers is how many servers a database holds (the client's server
// groups, cluster.MaxGroups).
const MaxServers = 10

// SchemaName is the schema with the world of a server (its group).
func SchemaName(server int) string { return fmt.Sprintf("s%d", server) }

// Server is the server (group) this store works on.
func (s *Store) Server() int { return s.server }

// Open connects as dbsrv: its connections may write accounts that are online
// in the game (see migration 0005, guard_online_account). Use it for dbsrv,
// the importer and tests.
//
// Open and OpenUnprivileged work on server 1 (tools and tests);
// OpenServer and OpenServerUnprivileged name the server.
func Open(ctx context.Context, dsn string, loc *time.Location) (*Store, error) {
	return open(ctx, dsn, loc, true, 1)
}

// OpenServer is Open for the world of a server (its group).
func OpenServer(ctx context.Context, dsn string, loc *time.Location, server int) (*Store, error) {
	return open(ctx, dsn, loc, true, server)
}

// OpenServerUnprivileged is OpenUnprivileged for the world of a server.
func OpenServerUnprivileged(ctx context.Context, dsn string, loc *time.Location, server int) (*Store, error) {
	return open(ctx, dsn, loc, false, server)
}

// OpenUnprivileged connects without the dbsrv writer flag, so the online
// guard applies (the admin panel).
func OpenUnprivileged(ctx context.Context, dsn string, loc *time.Location) (*Store, error) {
	return open(ctx, dsn, loc, false, 1)
}

func open(ctx context.Context, dsn string, loc *time.Location, writer bool, server int) (*Store, error) {
	if server < 1 || server > MaxServers {
		return nil, fmt.Errorf("store: server must be 1..%d, got %d", MaxServers, server)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	// The session default, so RESET search_path comes back to it.
	cfg.ConnConfig.RuntimeParams["search_path"] = SchemaName(server) + ", public"
	if writer {
		cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
			_, err := c.Exec(ctx, `SET dbsrv.writer = 'on'`)
			return err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, Loc: loc, server: server}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool exposes the connection pool for tests and diagnostics.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// ResetForTests drops every table. It refuses unless the database name ends
// in "_test", so a misconfigured DSN can never wipe real data.
func (s *Store) ResetForTests(ctx context.Context) error {
	var db string
	if err := s.pool.QueryRow(ctx, `SELECT current_database()`).Scan(&db); err != nil {
		return err
	}
	if !strings.HasSuffix(db, "_test") {
		return fmt.Errorf("store: refusing to wipe database %q (name must end in _test)", db)
	}
	q := `DROP SCHEMA IF EXISTS gamedata CASCADE; DROP SCHEMA public CASCADE; CREATE SCHEMA public;`
	for n := 1; n <= MaxServers; n++ {
		q += ` DROP SCHEMA IF EXISTS ` + SchemaName(n) + ` CASCADE;`
	}
	_, err := s.pool.Exec(ctx, q)
	return err
}

// LoadAccount reads an account with its characters and items on this server.
// An account that never played here has no world row yet: it comes with an
// empty storage and no characters.
func (s *Store) LoadAccount(ctx context.Context, username string) (*AccountRecord, error) {
	username = strings.ToUpper(username)
	var rec *AccountRecord
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		var id int64
		var r accountRow
		err := tx.QueryRow(ctx, `
			SELECT a.id, a.username, a.password_hash, a.status, a.numeric_pass_hash, a.pass_block, a.access_level,
			       a.cash, a.honra, COALESCE(w.novato, 0), COALESCE(w.storage_gold, 0), a.ban_type, a.ban_until,
			       COALESCE(w.already_reset, false), COALESCE(w.data, '{"CharSlot": -1}'::jsonb)
			FROM accounts a LEFT JOIN account_world w ON w.account_id = a.id
			WHERE a.username = $1`, username).Scan(
			&id, &r.Username, &r.PasswordHash, &r.Status, &r.NumericPassHash, &r.PassBlock, &r.AccessLevel,
			&r.Cash, &r.Honra, &r.Novato, &r.StorageGold, &r.BanType, &r.BanUntil, &r.AlreadyReset, &r.Data)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		rows, _ := tx.Query(ctx, `
			SELECT slot, name, class_info, face, evolution, level, exp, gold,
			       guild_index, guild_member_type, fame, citizen, data
			FROM characters WHERE account_id = $1`, id)
		chars, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (charRow, error) {
			var c charRow
			err := row.Scan(&c.Slot, &c.Name, &c.ClassInfo, &c.Face, &c.Evolution, &c.Level, &c.Exp, &c.Gold,
				&c.GuildIndex, &c.GuildMemberType, &c.Fame, &c.Citizen, &c.Data)
			return c, err
		})
		if err != nil {
			return err
		}

		rows, _ = tx.Query(ctx, `
			SELECT char_slot, container, slot, item_id, ef1, efv1, ef2, efv2, ef3, efv3
			FROM items WHERE account_id = $1`, id)
		items, err := pgx.CollectRows(rows, scanItemRow)
		if err != nil {
			return err
		}

		rec, err = mergeAccount(id, r, chars, items, s.Loc)
		return err
	})
	return rec, err
}

func scanItemRow(row pgx.CollectableRow) (itemRow, error) {
	var it itemRow
	var id, e1, v1, e2, v2, e3, v3 int32
	err := row.Scan(&it.CharSlot, &it.Container, &it.Slot, &id, &e1, &v1, &e2, &v2, &e3, &v3)
	it.Item.Index = uint16(id)
	it.Item.Effect[0].Index, it.Item.Effect[0].Value = uint8(e1), uint16(v1)
	it.Item.Effect[1].Index, it.Item.Effect[1].Value = uint8(e2), uint16(v2)
	it.Item.Effect[2].Index, it.Item.Effect[2].Value = uint8(e3), uint16(v3)
	return it, err
}

// SaveAccount writes the whole account (row, characters, items) in one
// transaction, inserting it if new. It does not touch character_names: name
// reservations change only on create/delete (ReserveName/ReleaseName).
// It sets rec.ID.
func (s *Store) SaveAccount(ctx context.Context, rec *AccountRecord) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return s.saveAccountTx(ctx, tx, rec)
	})
}

func (s *Store) saveAccountTx(ctx context.Context, tx pgx.Tx, rec *AccountRecord) error {
	row, chars, items, err := splitAccount(rec, s.Loc)
	if err != nil {
		return err
	}
	if rec.PasswordHash == "" {
		return fmt.Errorf("store: account %s has no password hash", row.Username)
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO accounts (username, password_hash, status, numeric_pass_hash, pass_block, access_level,
		                      cash, honra, ban_type, ban_until)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash, status = EXCLUDED.status,
			numeric_pass_hash = EXCLUDED.numeric_pass_hash, pass_block = EXCLUDED.pass_block,
			access_level = EXCLUDED.access_level, cash = EXCLUDED.cash, honra = EXCLUDED.honra,
			ban_type = EXCLUDED.ban_type, ban_until = EXCLUDED.ban_until,
			updated_at = now()
		RETURNING id`,
		row.Username, row.PasswordHash, row.Status, row.NumericPassHash, row.PassBlock, row.AccessLevel,
		row.Cash, row.Honra, row.BanType, row.BanUntil,
	).Scan(&id)
	if err != nil {
		return err
	}
	// The part of the account that belongs to this server's world.
	if _, err := tx.Exec(ctx, `
		INSERT INTO account_world (account_id, novato, storage_gold, already_reset, data)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (account_id) DO UPDATE SET
			novato = EXCLUDED.novato, storage_gold = EXCLUDED.storage_gold,
			already_reset = EXCLUDED.already_reset, data = EXCLUDED.data, updated_at = now()`,
		id, row.Novato, row.StorageGold, row.AlreadyReset, row.Data); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM characters WHERE account_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM items WHERE account_id = $1`, id); err != nil {
		return err
	}

	if len(chars) > 0 {
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"characters"},
			[]string{"account_id", "slot", "name", "class_info", "face", "evolution", "level", "exp", "gold",
				"guild_index", "guild_member_type", "fame", "citizen", "data"},
			pgx.CopyFromSlice(len(chars), func(i int) ([]any, error) {
				c := chars[i]
				return []any{id, c.Slot, c.Name, c.ClassInfo, c.Face, c.Evolution, c.Level, c.Exp, c.Gold,
					c.GuildIndex, c.GuildMemberType, c.Fame, c.Citizen, c.Data}, nil
			}))
		if err != nil {
			return err
		}
	}

	if len(items) > 0 {
		_, err = tx.CopyFrom(ctx, pgx.Identifier{"items"},
			[]string{"account_id", "char_slot", "container", "slot", "item_id", "ef1", "efv1", "ef2", "efv2", "ef3", "efv3"},
			pgx.CopyFromSlice(len(items), func(i int) ([]any, error) {
				it := items[i]
				e := it.Item.Effect
				return []any{id, it.CharSlot, it.Container, it.Slot, int32(it.Item.Index),
					int16(e[0].Index), int32(e[0].Value), int16(e[1].Index), int32(e[1].Value),
					int16(e[2].Index), int32(e[2].Value)}, nil
			}))
		if err != nil {
			return err
		}
	}
	rec.ID = id
	return nil
}

// ReserveName claims a character name for an account, replacing the
// char/<L>/<NAME> file. It reports false if the name is taken. With
// allowOwn, a name the account already owns counts as success (an Arch
// reuses its mortal's name); without it any existing reservation fails,
// as the C++ CreateCharacter did when the file existed.
func (s *Store) ReserveName(ctx context.Context, accountID int64, name string, allowOwn bool) (bool, error) {
	// A concurrent reserver can commit between our INSERT's conflict check and
	// the SELECT's snapshot, leaving both branches empty; just try again.
	for attempt := 0; ; attempt++ {
		inserted, owner, err := s.reserveNameOnce(ctx, accountID, name)
		if errors.Is(err, pgx.ErrNoRows) && attempt < 3 {
			continue
		}
		if err != nil {
			return false, err
		}
		return inserted || (allowOwn && owner == accountID), nil
	}
}

func (s *Store) reserveNameOnce(ctx context.Context, accountID int64, name string) (inserted bool, owner int64, err error) {
	err = s.pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO character_names (name_key, account_id) VALUES (lower($1), $2)
			ON CONFLICT (name_key) DO NOTHING RETURNING account_id)
		SELECT true, account_id FROM ins
		UNION ALL SELECT false, account_id FROM character_names WHERE name_key = lower($1)
		LIMIT 1`, name, accountID).Scan(&inserted, &owner)
	return inserted, owner, err
}

// SetNumericPassHash stores the numeric second password (0xFDE).
func (s *Store) SetNumericPassHash(ctx context.Context, accountID int64, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE accounts SET numeric_pass_hash = $2, updated_at = now() WHERE id = $1`, accountID, hash)
	return err
}

// ReleaseName frees a name if this account owns it.
func (s *Store) ReleaseName(ctx context.Context, accountID int64, name string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM character_names WHERE name_key = lower($1) AND account_id = $2`, name, accountID)
	return err
}

// NameOwner returns the account that owns a character name.
func (s *Store) NameOwner(ctx context.Context, name string) (string, error) {
	var user string
	err := s.pool.QueryRow(ctx, `
		SELECT a.username FROM character_names n JOIN accounts a ON a.id = n.account_id
		WHERE n.name_key = lower($1)`, name).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return user, err
}

// serverLockBase + server is the advisory lock a DBSRV holds while it serves
// a server.
const serverLockBase = 7514_1000

// LockServer makes this process the only DBSRV of the store's server: two of
// them would each keep its own copy of the accounts and overwrite the other.
// It fails when another DBSRV holds the server. The lock lives on its own
// connection until release; if that connection drops, it is taken again as
// soon as the database answers, and lost reports when that fails because
// another DBSRV took the server meanwhile.
func (s *Store) LockServer(ctx context.Context, lost func(error)) (release func(), err error) {
	key := serverLockBase + s.server
	take := func(ctx context.Context) (*pgx.Conn, error) {
		conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig)
		if err != nil {
			return nil, err
		}
		var ok bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&ok); err != nil || !ok {
			_ = conn.Close(context.WithoutCancel(ctx))
			if err == nil {
				err = fmt.Errorf("another DBSRV is already serving server %d on this database", s.server)
			}
			return nil, err
		}
		return conn, nil
	}
	conn, err := take(ctx)
	if err != nil {
		return nil, err
	}
	hold, stop := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-hold.Done():
				if conn != nil {
					_ = conn.Close(context.Background())
				}
				return
			case <-tick.C:
			}
			if conn != nil {
				if conn.Ping(hold) == nil {
					continue
				}
				_ = conn.Close(context.Background())
				conn = nil
			}
			if c, err := take(hold); err == nil {
				conn = c
			} else if hold.Err() == nil && lost != nil {
				lost(err)
			}
		}
	}()
	return func() { stop(); <-done }, nil
}
