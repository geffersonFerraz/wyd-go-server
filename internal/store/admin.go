package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"dbsrv/internal/acctedit"

	"github.com/jackc/pgx/v5"
)

// AccountSummary is one row of the admin account list.
type AccountSummary struct {
	Username    string
	Status      int16
	AccessLevel int32
	Cash        int64
	HasNumeric  bool
	BanType     int32
	BanUntil    *time.Time
	Characters  []string
	Online      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CharacterSummary is a character as shown in the admin panel.
type CharacterSummary struct {
	Slot      int16
	Name      string
	Level     int32
	Evolution int16
	Gold      int64
	Guild     string
}

// SearchAccounts finds accounts whose username or character name contains q
// (case-insensitive). An empty q lists the most recently updated.
func (s *Store) SearchAccounts(ctx context.Context, q string, limit int) ([]AccountSummary, error) {
	q = strings.TrimSpace(q)
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
	rows, _ := s.pool.Query(ctx, `
		SELECT a.username, a.status, a.access_level, a.cash, a.numeric_pass_hash IS NOT NULL,
		       a.ban_type, a.ban_until, a.created_at, a.updated_at,
		       COALESCE(array_agg(c.name ORDER BY c.slot) FILTER (WHERE c.name IS NOT NULL), '{}'),
		       EXISTS (SELECT 1 FROM online_accounts o WHERE o.account_id = a.id)
		FROM accounts a
		LEFT JOIN characters c ON c.account_id = a.id
		WHERE $1 = '%%'
		   OR lower(a.username) LIKE $1
		   OR EXISTS (SELECT 1 FROM characters c2 WHERE c2.account_id = a.id AND lower(c2.name) LIKE $1)
		GROUP BY a.id
		ORDER BY a.updated_at DESC, a.username
		LIMIT $2`, pattern, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AccountSummary, error) {
		var a AccountSummary
		err := r.Scan(&a.Username, &a.Status, &a.AccessLevel, &a.Cash, &a.HasNumeric,
			&a.BanType, &a.BanUntil, &a.CreatedAt, &a.UpdatedAt, &a.Characters, &a.Online)
		return a, err
	})
}

// AccountDetail returns one account's summary and characters.
func (s *Store) AccountDetail(ctx context.Context, username string) (AccountSummary, []CharacterSummary, error) {
	var a AccountSummary
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, status, access_level, cash, numeric_pass_hash IS NOT NULL,
		       ban_type, ban_until, created_at, updated_at
		FROM accounts WHERE username = $1`, strings.ToUpper(username)).Scan(
		&id, &a.Username, &a.Status, &a.AccessLevel, &a.Cash, &a.HasNumeric,
		&a.BanType, &a.BanUntil, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil, ErrNotFound
	}
	if err != nil {
		return a, nil, err
	}
	rows, _ := s.pool.Query(ctx, `
		SELECT c.slot, c.name, c.level, c.evolution, c.gold, COALESCE(g.name, '')
		FROM characters c LEFT JOIN guilds g ON g.id = c.guild_index
		WHERE c.account_id = $1 ORDER BY c.slot`, id)
	chars, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CharacterSummary, error) {
		var c CharacterSummary
		err := r.Scan(&c.Slot, &c.Name, &c.Level, &c.Evolution, &c.Gold, &c.Guild)
		return c, err
	})
	return a, chars, err
}

// DeliveryProtocol is the deliveries payload version this code base speaks.
// 2 = password_hash / numeric_hash payloads and token-only changes.
// 3 = "kick" deliveries and online_accounts tracking.
// 4 = "edit" deliveries (package acctedit).
// 5 = the "lock" edit (reload the in-game account lock).
// Bump it whenever dbsrv learns a payload the admin panel starts sending.
const DeliveryProtocol = 5

// OnlineInfo says where an account is logged in.
type OnlineInfo struct {
	Channel int
	Client  int
	Since   time.Time
}

// SetOnline records a login (replacing any stale row for the account).
func (s *Store) SetOnline(ctx context.Context, username string, channel, client int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO online_accounts (account_id, username, channel, client)
		SELECT id, username, $2, $3 FROM accounts WHERE username = $1
		ON CONFLICT (account_id) DO UPDATE SET channel = EXCLUDED.channel, client = EXCLUDED.client, since = now()`,
		strings.ToUpper(username), channel, client)
	return err
}

// SetOffline removes the row only if it is still this channel/client's
// login, so a late logout cannot erase a newer login elsewhere.
func (s *Store) SetOffline(ctx context.Context, username string, channel, client int) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM online_accounts WHERE username = $1 AND channel = $2 AND client = $3`,
		strings.ToUpper(username), channel, client)
	return err
}

// ClearOnline removes every row for a channel, or all rows when channel < 0
// (dbsrv startup: nobody can be online yet).
func (s *Store) ClearOnline(ctx context.Context, channel int) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM online_accounts WHERE $1 < 0 OR channel = $1`, channel)
	return err
}

// Online returns where username is logged in, or nil.
func (s *Store) Online(ctx context.Context, username string) (*OnlineInfo, error) {
	var o OnlineInfo
	err := s.pool.QueryRow(ctx, `SELECT channel, client, since FROM online_accounts WHERE username = $1`,
		strings.ToUpper(username)).Scan(&o.Channel, &o.Client, &o.Since)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// Heartbeat records that dbsrv is alive and which payload version it speaks.
func (s *Store) Heartbeat(ctx context.Context, protocol int) error {
	_, err := s.pool.Exec(ctx, `UPDATE server_state SET dbsrv_protocol = $1, dbsrv_seen_at = now() WHERE id = 1`, protocol)
	return err
}

// ReportChannels records the online count of each channel (-1 when its
// TMSRV is not connected), sent by dbsrv with its heartbeat.
func (s *Store) ReportChannels(ctx context.Context, online []int) error {
	counts := make([]int32, len(online))
	for i, n := range online {
		counts[i] = int32(n)
	}
	_, err := s.pool.Exec(ctx, `UPDATE server_state SET channel_online = $1 WHERE id = 1`, counts)
	return err
}

// ChannelStatus returns the last ReportChannels counts and the age of
// dbsrv's heartbeat (-1 when it never beat), measured by the database clock.
func (s *Store) ChannelStatus(ctx context.Context) (online []int, age time.Duration, err error) {
	var counts []int32
	var ageSec *float64
	err = s.pool.QueryRow(ctx, `
		SELECT channel_online, EXTRACT(EPOCH FROM now() - dbsrv_seen_at)::float8
		FROM server_state WHERE id = 1`).Scan(&counts, &ageSec)
	if err != nil {
		return nil, 0, err
	}
	age = -1
	if ageSec != nil {
		age = time.Duration(*ageSec * float64(time.Second))
	}
	online = make([]int, len(counts))
	for i, n := range counts {
		online[i] = int(n)
	}
	return online, age, nil
}

// DBSRVStatus returns dbsrv's last heartbeat (seenAt nil if never) and
// how long ago it was, measured by the database clock.
func (s *Store) DBSRVStatus(ctx context.Context) (protocol int, seenAt *time.Time, age time.Duration, err error) {
	var ageSec *float64
	err = s.pool.QueryRow(ctx, `
		SELECT dbsrv_protocol, dbsrv_seen_at, EXTRACT(EPOCH FROM now() - dbsrv_seen_at)::float8
		FROM server_state WHERE id = 1`).Scan(&protocol, &seenAt, &ageSec)
	if ageSec != nil {
		age = time.Duration(*ageSec * float64(time.Second))
	}
	return protocol, seenAt, age, err
}

// AccountExists reports whether a username is taken.
func (s *Store) AccountExists(ctx context.Context, username string) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM accounts WHERE username = $1)`, strings.ToUpper(username)).Scan(&ok)
	return ok, err
}

// DeliveryInfo is a deliveries row for display. The payload is not
// returned: it can hold a new password.
type DeliveryInfo struct {
	ID          int64
	Kind        string
	Username    string
	RequestedBy string
	CreatedAt   time.Time
	ProcessedAt *time.Time
	Result      string
	Attempts    int
	// Item deliveries only: the item and its effects.
	ItemID   int
	ItemName string
	Effects  [][2]int
	// Edit deliveries only: the change.
	Edit *acctedit.Edit
}

const deliveryCols = `id, kind, username, requested_by, created_at, processed_at, COALESCE(result, ''), attempts,
	CASE WHEN kind = 'item' AND payload->>'item_id' ~ '^[0-9]{1,5}$' THEN (payload->>'item_id')::int ELSE 0 END,
	CASE WHEN kind = 'item' AND payload->>'item_id' ~ '^[0-9]{1,5}$'
		THEN COALESCE((SELECT name FROM gamedata.items i WHERE i.id = (payload->>'item_id')::int), '') ELSE '' END,
	CASE WHEN kind = 'item' THEN payload->'effects' END,
	CASE WHEN kind = 'edit' THEN payload END`

func scanDelivery(r pgx.CollectableRow) (DeliveryInfo, error) {
	var d DeliveryInfo
	var effects, edit []byte
	err := r.Scan(&d.ID, &d.Kind, &d.Username, &d.RequestedBy, &d.CreatedAt, &d.ProcessedAt, &d.Result, &d.Attempts,
		&d.ItemID, &d.ItemName, &effects, &edit)
	if err == nil && len(effects) > 0 {
		_ = json.Unmarshal(effects, &d.Effects) // display only
	}
	if err == nil && len(edit) > 0 {
		d.Edit = &acctedit.Edit{}
		_ = json.Unmarshal(edit, d.Edit)
	}
	return d, err
}

// RecentDeliveries lists deliveries, newest first; username "" means all.
func (s *Store) RecentDeliveries(ctx context.Context, username string, limit int) ([]DeliveryInfo, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+deliveryCols+` FROM deliveries
		WHERE $1 = '' OR username = $1 ORDER BY id DESC LIMIT $2`, strings.ToUpper(username), limit)
	return pgx.CollectRows(rows, scanDelivery)
}

func (s *Store) DeliveryByID(ctx context.Context, id int64) (DeliveryInfo, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE id = $1`, id)
	d, err := pgx.CollectExactlyOneRow(rows, scanDelivery)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}
