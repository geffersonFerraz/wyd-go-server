package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GMMessage is what a player wrote with /gm.
type GMMessage struct {
	ID        int64
	CreatedAt time.Time
	Channel   int
	Username  string
	CharName  string
	Text      string
	SeenAt    *time.Time
	SeenBy    string
}

// AddGMMessage stores a /gm message.
func (s *Store) AddGMMessage(ctx context.Context, m GMMessage) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO gm_messages (channel, username, char_name, text) VALUES ($1, $2, $3, $4)`,
		m.Channel, strings.ToUpper(m.Username), m.CharName, m.Text)
	return err
}

// GMMessages lists the newest messages (onlyUnseen: those not marked as read).
func (s *Store) GMMessages(ctx context.Context, onlyUnseen bool, limit int) ([]GMMessage, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, created_at, channel, username, char_name, text, seen_at, seen_by
		FROM gm_messages WHERE NOT $1 OR seen_at IS NULL ORDER BY id DESC LIMIT $2`, onlyUnseen, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (GMMessage, error) {
		var m GMMessage
		err := r.Scan(&m.ID, &m.CreatedAt, &m.Channel, &m.Username, &m.CharName, &m.Text, &m.SeenAt, &m.SeenBy)
		return m, err
	})
}

// UnseenGMMessages counts the messages not read yet.
func (s *Store) UnseenGMMessages(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM gm_messages WHERE seen_at IS NULL`).Scan(&n)
	return n, err
}

// MarkGMMessagesSeen marks one message as read (id 0 = all of them).
func (s *Store) MarkGMMessagesSeen(ctx context.Context, id int64, by string) error {
	_, err := s.pool.Exec(ctx, `UPDATE gm_messages SET seen_at = now(), seen_by = $2
		WHERE seen_at IS NULL AND ($1 = 0 OR id = $1)`, id, by)
	return err
}

// GMCommandUser is the username of the gm_command deliveries.
const GMCommandUser = "*GM"

// GMCommand is a command sent from the panel and what the channel answered.
type GMCommand struct {
	ID          int64
	RequestedBy string
	CreatedAt   time.Time
	Done        bool
	Result      string
	Channel     int    `json:"channel"`
	Char        string `json:"char_name"`
	Command     string `json:"command"`
	Output      string `json:"output"`
}

// GMCommands lists the newest commands.
func (s *Store) GMCommands(ctx context.Context, limit int) ([]GMCommand, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, requested_by, created_at, processed_at IS NOT NULL, COALESCE(result, ''), payload
		FROM deliveries WHERE kind = 'gm_command' AND server = $2 ORDER BY id DESC LIMIT $1`, limit, s.server)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (GMCommand, error) {
		var c GMCommand
		var raw []byte
		if err := r.Scan(&c.ID, &c.RequestedBy, &c.CreatedAt, &c.Done, &c.Result, &raw); err != nil {
			return c, err
		}
		_ = json.Unmarshal(raw, &c) // display only
		return c, nil
	})
}

// SetGMCommandOutput keeps what the channel answered. It only touches the
// payload, so it may arrive before or after FinishDelivery.
func (s *Store) SetGMCommandOutput(ctx context.Context, id int64, output string) error {
	_, err := s.pool.Exec(ctx, `UPDATE deliveries SET payload = payload || jsonb_build_object('output', $2::text)
		WHERE id = $1 AND kind = 'gm_command'`, id, output)
	return err
}
