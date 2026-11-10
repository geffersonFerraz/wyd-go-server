package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"dbsrv/internal/wire"
)

// Guild relation kinds (guild_relations.kind).
const (
	RelationAlly = 1
	RelationWar  = 2
)

func (s *Store) SetGuildRelation(ctx context.Context, guild, kind, other int) error {
	if other == 0 {
		_, err := s.pool.Exec(ctx, `DELETE FROM guild_relations WHERE guild_id = $1 AND kind = $2`, guild, kind)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO guild_relations (guild_id, kind, other_guild_id) VALUES ($1, $2, $3)
		ON CONFLICT (guild_id, kind) DO UPDATE SET other_guild_id = EXCLUDED.other_guild_id`, guild, kind, other)
	return err
}

// GuildRelations returns guild -> other for one kind.
func (s *Store) GuildRelations(ctx context.Context, kind int) (map[int]int, error) {
	rows, _ := s.pool.Query(ctx, `SELECT guild_id, other_guild_id FROM guild_relations WHERE kind = $1`, kind)
	out := map[int]int{}
	var g, o int
	_, err := pgx.ForEachRow(rows, []any{&g, &o}, func() error { out[g] = o; return nil })
	return out, err
}

func (s *Store) DonateStore(ctx context.Context) ([]DonateSlot, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT store, slot, price, loop, available, item_id, ef1, efv1, ef2, efv2, ef3, efv3
		FROM donate_store ORDER BY store, slot`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DonateSlot, error) {
		var d DonateSlot
		var id, e1, v1, e2, v2, e3, v3 int32
		err := r.Scan(&d.Store, &d.Slot, &d.Price, &d.Loop, &d.Available, &id, &e1, &v1, &e2, &v2, &e3, &v3)
		d.Item = wire.Item{Index: uint16(id), Effect: [3]wire.ItemEffect{
			{Index: uint8(e1), Value: uint16(v1)}, {Index: uint8(e2), Value: uint16(v2)}, {Index: uint8(e3), Value: uint16(v3)}}}
		return d, err
	})
}

func (s *Store) SetDonateAvailable(ctx context.Context, store, slot, available int) error {
	_, err := s.pool.Exec(ctx, `UPDATE donate_store SET available = $3 WHERE store = $1 AND slot = $2`, store, slot, available)
	return err
}

// KefraState is the weekly first-kill record.
type KefraState struct {
	FirstChannel int // 10 = nobody yet
	Killer       string
}

func (s *Store) Kefra(ctx context.Context) (KefraState, error) {
	var k KefraState
	err := s.pool.QueryRow(ctx, `SELECT first_kefra_channel, kefra_killer FROM server_state WHERE id = 1`).Scan(&k.FirstChannel, &k.Killer)
	return k, err
}

func (s *Store) SetKefra(ctx context.Context, k KefraState) error {
	_, err := s.pool.Exec(ctx, `UPDATE server_state SET first_kefra_channel = $1, kefra_killer = $2 WHERE id = 1`, k.FirstChannel, k.Killer)
	return err
}

// Seal reads a sealed character.
func (s *Store) Seal(ctx context.Context, id int) (wire.SealInfo, wire.CharInfo, error) {
	var info wire.SealInfo
	var char wire.CharInfo
	var infoJSON, charJSON []byte
	err := s.pool.QueryRow(ctx, `SELECT info, character FROM seals WHERE id = $1`, id).Scan(&infoJSON, &charJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return info, char, ErrNotFound
	}
	if err != nil {
		return info, char, err
	}
	if err := json.Unmarshal(infoJSON, &info); err != nil {
		return info, char, err
	}
	err = json.Unmarshal(charJSON, &char)
	return info, char, err
}

func (s *Store) NextSealID(ctx context.Context) (int, error) {
	var id int
	err := s.pool.QueryRow(ctx, `SELECT nextval('seal_id_seq')`).Scan(&id)
	return id, err
}

func (s *Store) MarkSealUsed(ctx context.Context, id int) error {
	_, err := s.pool.Exec(ctx, `UPDATE seals SET used_at = now() WHERE id = $1`, id)
	return err
}

// Delivery is one deliveries row.
type Delivery struct {
	ID       int64
	Kind     string
	Username string
	Payload  json.RawMessage
	Attempts int
}

// PendingDeliveries returns unprocessed deliveries, oldest first.
func (s *Store) PendingDeliveries(ctx context.Context, limit int) ([]Delivery, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT id, kind, username, payload, attempts FROM deliveries
		WHERE processed_at IS NULL ORDER BY id LIMIT $1`, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) {
		var d Delivery
		err := r.Scan(&d.ID, &d.Kind, &d.Username, &d.Payload, &d.Attempts)
		d.Username = strings.ToUpper(strings.TrimSpace(d.Username))
		return d, err
	})
}

// FinishDelivery marks a delivery done with a result code.
func (s *Store) FinishDelivery(ctx context.Context, id int64, result string) error {
	// Secrets (plain or hashed) are not kept once applied.
	_, err := s.pool.Exec(ctx, `
		UPDATE deliveries SET processed_at = now(), result = $2, attempts = attempts + 1,
			payload = payload - 'password' - 'numeric' - 'password_hash' - 'numeric_hash'
		WHERE id = $1`, id, result)
	return err
}

// DeferDelivery records an attempt that must be retried later.
func (s *Store) DeferDelivery(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE deliveries SET attempts = attempts + 1, result = $2 WHERE id = $1`, id, reason)
	return err
}

// Enqueue adds a delivery for DBSRV to process. requestedBy is recorded for audit.
func (s *Store) Enqueue(ctx context.Context, kind, username string, payload any, requestedBy string) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO deliveries (kind, username, payload, requested_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		kind, strings.ToUpper(username), raw, requestedBy).Scan(&id)
	return id, err
}

// DeliveryResult returns processed_at != NULL and the result text.
func (s *Store) DeliveryResult(ctx context.Context, id int64) (bool, string, error) {
	var done bool
	var res *string
	err := s.pool.QueryRow(ctx, `SELECT processed_at IS NOT NULL, result FROM deliveries WHERE id = $1`, id).Scan(&done, &res)
	if res == nil {
		return done, "", err
	}
	return done, *res, err
}

// CreateAccount inserts a new empty account. It reports false if the
// username exists (the C++ ImportUser silently overwrote it with a blank account).
func (s *Store) CreateAccount(ctx context.Context, username, passwordHash string, numericHash *string, access int) (bool, error) {
	// data: CharSlot -1 like a fresh C++ account file.
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO accounts (username, password_hash, numeric_pass_hash, access_level, data)
		VALUES ($1, $2, $3, $4, '{"CharSlot": -1}')
		ON CONFLICT (username) DO NOTHING`, strings.ToUpper(username), passwordHash, numericHash, access)
	return tag.RowsAffected() == 1, err
}

// CreateAccountWithEmail is CreateAccount for the public registration page,
// at access level 0. A nil email leaves the account without one.
func (s *Store) CreateAccountWithEmail(ctx context.Context, username, passwordHash string, numericHash, email *string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO accounts (username, password_hash, numeric_pass_hash, email, data)
		VALUES ($1, $2, $3, $4, '{"CharSlot": -1}')
		ON CONFLICT (username) DO NOTHING`, strings.ToUpper(username), passwordHash, numericHash, email)
	return tag.RowsAffected() == 1, err
}

// SetPassword updates the password hash and/or the numeric password hash;
// a nil argument leaves that one unchanged. It reports false if the
// account does not exist.
func (s *Store) SetPassword(ctx context.Context, username string, hash, numericHash *string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE accounts SET
			password_hash     = COALESCE($2::text, password_hash),
			numeric_pass_hash = COALESCE($3::text, numeric_pass_hash),
			updated_at = now()
		WHERE username = $1`, strings.ToUpper(username), hash, numericHash)
	return tag.RowsAffected() == 1, err
}

// SetBan updates ban columns only. until nil clears the ban.
func (s *Store) SetBan(ctx context.Context, username string, banType int, until *time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE accounts SET ban_type = $2, ban_until = $3, updated_at = now() WHERE username = $1`,
		strings.ToUpper(username), banType, until)
	return tag.RowsAffected() == 1, err
}

// AddCash adds to an offline account's cash atomically.
func (s *Store) AddCash(ctx context.Context, username string, amount int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE accounts SET cash = LEAST(cash + $2, 4294967295), updated_at = now() WHERE username = $1`,
		strings.ToUpper(username), amount)
	return tag.RowsAffected() == 1, err
}

// PutInStorage places an item in the first free storage slot (0..159, as
// the C++ ImportItem) of an offline account. Returns the slot, -1 if full.
func (s *Store) PutInStorage(ctx context.Context, username string, it wire.Item) (int, error) {
	slot := -1
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE username = $1 FOR UPDATE`, strings.ToUpper(username)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			SELECT s FROM generate_series(0, 159) s
			WHERE NOT EXISTS (SELECT 1 FROM items WHERE account_id = $1 AND char_slot = -1 AND container = 2 AND slot = s)
			ORDER BY s LIMIT 1`, id).Scan(&slot)
		if errors.Is(err, pgx.ErrNoRows) {
			slot = -1
			return nil
		}
		if err != nil {
			return err
		}
		e := it.Effect
		_, err = tx.Exec(ctx, `
			INSERT INTO items (account_id, char_slot, container, slot, item_id, ef1, efv1, ef2, efv2, ef3, efv3)
			VALUES ($1, -1, 2, $2, $3, $4, $5, $6, $7, $8, $9)`,
			id, slot, int32(it.Index), int16(e[0].Index), int32(e[0].Value), int16(e[1].Index), int32(e[1].Value),
			int16(e[2].Index), int32(e[2].Value))
		return err
	})
	return slot, err
}
