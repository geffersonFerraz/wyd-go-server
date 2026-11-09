package store

import (
	"context"
	"encoding/json"
	"strings"
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

// StoreSale is one sale in the auction stores.
type StoreSale struct {
	ID            int64
	SoldAt        time.Time
	Channel       int
	Buyer, Seller string
	Item          int
	ItemName      string
	Effects       [3]ItemEffect
	Price, Tax    int64
	Category      int // 1 donate, 2 gold, 3 bars
}

// AddStoreSale records a sale.
func (s *Store) AddStoreSale(ctx context.Context, v StoreSale) error {
	raw, err := json.Marshal(v.Effects)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO store_sales (channel, buyer, seller, item, effects, price, tax, category)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, v.Channel, v.Buyer, v.Seller, v.Item, raw, v.Price, v.Tax, v.Category)
	return err
}

// StoreSales lists the newest sales; q filters by buyer or seller.
func (s *Store) StoreSales(ctx context.Context, q string, limit int) ([]StoreSale, error) {
	rows, _ := s.pool.Query(ctx, `SELECT v.id, v.sold_at, v.channel, v.buyer, v.seller, v.item, COALESCE(i.name, ''), v.effects, v.price, v.tax, v.category
		FROM store_sales v LEFT JOIN gamedata.items i ON i.id = v.item
		WHERE $1 = '' OR lower(v.buyer) = $1 OR lower(v.seller) = $1 ORDER BY v.id DESC LIMIT $2`, strings.ToLower(strings.TrimSpace(q)), limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StoreSale, error) {
		var v StoreSale
		var raw []byte
		if err := r.Scan(&v.ID, &v.SoldAt, &v.Channel, &v.Buyer, &v.Seller, &v.Item, &v.ItemName, &raw, &v.Price, &v.Tax, &v.Category); err != nil {
			return v, err
		}
		_ = json.Unmarshal(raw, &v.Effects) // display only
		return v, nil
	})
}
