package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// SetMacBan bans or frees a computer.
func (s *Store) SetMacBan(ctx context.Context, mac [8]byte, banned bool) error {
	var err error
	if banned {
		_, err = s.pool.Exec(ctx, `INSERT INTO mac_bans (mac) VALUES ($1) ON CONFLICT DO NOTHING`, mac[:])
	} else {
		_, err = s.pool.Exec(ctx, `DELETE FROM mac_bans WHERE mac = $1`, mac[:])
	}
	return err
}

// MacBanRow is a banned computer for the panel.
type MacBanRow struct {
	MAC       string
	CreatedAt time.Time
}

// MacBanRows lists the banned computers, newest first.
func (s *Store) MacBanRows(ctx context.Context) ([]MacBanRow, error) {
	rows, _ := s.pool.Query(ctx, `SELECT encode(mac, 'hex'), created_at FROM mac_bans ORDER BY created_at DESC`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (MacBanRow, error) {
		var m MacBanRow
		return m, r.Scan(&m.MAC, &m.CreatedAt)
	})
}

// MacBans lists the banned computers.
func (s *Store) MacBans(ctx context.Context) ([][8]byte, error) {
	rows, err := s.pool.Query(ctx, `SELECT mac FROM mac_bans`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) ([8]byte, error) {
		var raw []byte
		var mac [8]byte
		err := r.Scan(&raw)
		copy(mac[:], raw)
		return mac, err
	})
}
