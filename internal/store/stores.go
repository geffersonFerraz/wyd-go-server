package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// OpenStore is a store open in the game.
type OpenStore struct {
	Channel   int
	Owner     string
	Name      string
	Items     int
	UpdatedAt time.Time
}

// TouchOpenStore records that a store is open now.
func (s *Store) TouchOpenStore(ctx context.Context, channel int, owner, name string, items int) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO open_stores (channel, owner_name, store_name, items) VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel, owner_name) DO UPDATE SET store_name = EXCLUDED.store_name, items = EXCLUDED.items, updated_at = now()`,
		channel, owner, name, items)
	return err
}

// OpenStores lists the stores reported in the last 3 minutes.
func (s *Store) OpenStores(ctx context.Context) ([]OpenStore, error) {
	rows, _ := s.pool.Query(ctx, `SELECT channel, owner_name, store_name, items, updated_at FROM open_stores
		WHERE updated_at > now() - interval '3 minutes' ORDER BY channel, owner_name`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (OpenStore, error) {
		var o OpenStore
		err := r.Scan(&o.Channel, &o.Owner, &o.Name, &o.Items, &o.UpdatedAt)
		return o, err
	})
}
