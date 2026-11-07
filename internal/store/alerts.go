package store

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Alert is one alerts row: something a TMSRV reported for the staff.
type Alert struct {
	ID        int64
	CreatedAt time.Time
	Kind      string // "wpe", ...
	Username  string
	CharName  string
	IP        string
	Channel   int
	Detail    string
	SeenAt    *time.Time
	SeenBy    string
}

// Alert kinds as stored.
const AlertKindWPE = "wpe"

// AddAlert stores an alert.
func (s *Store) AddAlert(ctx context.Context, a Alert) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO alerts (kind, username, char_name, ip, channel, detail)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		a.Kind, strings.ToUpper(a.Username), a.CharName, a.IP, a.Channel, a.Detail).Scan(&id)
	return id, err
}

// Alerts lists the newest alerts (onlyUnseen: those not marked as seen).
func (s *Store) Alerts(ctx context.Context, onlyUnseen bool, limit int) ([]Alert, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, created_at, kind, username, char_name, ip, channel, detail, seen_at, seen_by
		FROM alerts WHERE NOT $1 OR seen_at IS NULL ORDER BY id DESC LIMIT $2`, onlyUnseen, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Alert, error) {
		var a Alert
		err := r.Scan(&a.ID, &a.CreatedAt, &a.Kind, &a.Username, &a.CharName, &a.IP, &a.Channel, &a.Detail, &a.SeenAt, &a.SeenBy)
		return a, err
	})
}

// UnseenAlerts counts the alerts not marked as seen.
func (s *Store) UnseenAlerts(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE seen_at IS NULL`).Scan(&n)
	return n, err
}

// MarkAlertsSeen marks one alert (id > 0) or all of them (id 0) as seen.
func (s *Store) MarkAlertsSeen(ctx context.Context, id int64, by string) error {
	_, err := s.pool.Exec(ctx, `UPDATE alerts SET seen_at = now(), seen_by = $2
		WHERE seen_at IS NULL AND ($1 = 0 OR id = $1)`, id, by)
	return err
}
