package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var rateDropCols = []string{"drop_name", "slot", "item", "effect1", "value1", "effect2", "value2", "effect3", "value3", "chance"}

const rateDropSelect = `drop_name, slot, item, effect1, value1, effect2, value2, effect3, value3, chance`

func rateDropRow(name string, r RateDrop) []any {
	return []any{name, r.Slot, r.Item, r.Effects[0].Effect, r.Effects[0].Value, r.Effects[1].Effect, r.Effects[1].Value,
		r.Effects[2].Effect, r.Effects[2].Value, r.Chance}
}

func rateDropScan(r *RateDrop) []any {
	return []any{&r.Slot, &r.Item, &r.Effects[0].Effect, &r.Effects[0].Value, &r.Effects[1].Effect, &r.Effects[1].Value,
		&r.Effects[2].Effect, &r.Effects[2].Value, &r.Chance}
}

func insertRateDrops(ctx context.Context, tx pgx.Tx, name string, list []RateDrop) error {
	if _, err := tx.Exec(ctx, `DELETE FROM gamedata.rate_drops WHERE drop_name = $1`, name); err != nil {
		return err
	}
	for _, r := range list {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.rate_drops (`+rateDropSelect+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, rateDropRow(name, r)...); err != nil {
			return err
		}
	}
	return nil
}

// RateDropsImported reports whether the monsters' inventories were copied.
func (s *Store) RateDropsImported(ctx context.Context) (bool, error) {
	var at int
	err := s.pool.QueryRow(ctx, `SELECT id FROM gamedata.rate_drop_import`).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// SeedRateDrops copies the monsters' inventories once. A monster already in
// the table keeps its fixed list. It returns how many lines it wrote.
func (s *Store) SeedRateDrops(ctx context.Context, drops []Drop, by string) (int, error) {
	lines := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO gamedata.rate_drop_import (imported_by) VALUES ($1) ON CONFLICT (id) DO NOTHING`, by)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil // another panel did it
		}
		for _, d := range drops {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.drops (name, rate_gold, sort)
				VALUES ($1, $2, (SELECT coalesce(max(sort), 0) + 1 FROM gamedata.drops))
				ON CONFLICT (name) DO UPDATE SET rate_gold = EXCLUDED.rate_gold`, d.Name, d.RateGold); err != nil {
				return fmt.Errorf("%q: %w", d.Name, err)
			}
			if err := insertRateDrops(ctx, tx, d.Name, d.Rate); err != nil {
				return fmt.Errorf("%q: %w", d.Name, err)
			}
			lines += len(d.Rate)
		}
		return logChange(ctx, tx, by, "import", "drops por chance", fmt.Sprintf("%d monstros, %d itens", len(drops), lines))
	})
	return lines, err
}

// DropSlots returns the 64 slots.
func (s *Store) DropSlots(ctx context.Context) ([]DropSlot, error) {
	rows, _ := s.pool.Query(ctx, `SELECT slot, base, rate FROM gamedata.drop_slots ORDER BY slot`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DropSlot, error) {
		var d DropSlot
		return d, r.Scan(&d.Slot, &d.Base, &d.Rate)
	})
}

// BlockedDropItems lists the items that never drop by chance.
func (s *Store) BlockedDropItems(ctx context.Context) ([]int, error) {
	rows, _ := s.pool.Query(ctx, `SELECT item FROM gamedata.blocked_drop_items ORDER BY item`)
	return pgx.CollectRows(rows, pgx.RowTo[int])
}

// SaveDropRates replaces the slot settings and the blocked items.
func (s *Store) SaveDropRates(ctx context.Context, slots []DropSlot, blocked []int, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, d := range slots {
			if _, err := tx.Exec(ctx, `UPDATE gamedata.drop_slots SET base = $2, rate = $3 WHERE slot = $1`, d.Slot, d.Base, d.Rate); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM gamedata.blocked_drop_items`); err != nil {
			return err
		}
		for _, id := range blocked {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.blocked_drop_items (item) VALUES ($1) ON CONFLICT DO NOTHING`, id); err != nil {
				return err
			}
		}
		return logChange(ctx, tx, by, "droprates", "todas", fmt.Sprintf("%d itens bloqueados", len(blocked)))
	})
}
