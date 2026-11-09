package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// errDryRun rolls a checked import back.
var errDryRun = errors.New("dry run")

// ImportServerConfig replaces all the game configuration in one transaction:
// the gamedata tables, the drop slots and blocked items, the city owners and
// the computer bans. With dryRun everything runs and is then undone, so the
// database itself checks the data.
func (s *Store) ImportServerConfig(ctx context.Context, gd *GameData, macBans [][8]byte, by string, dryRun bool) error {
	if len(gd.DropSlots) != 64 {
		return fmt.Errorf("drop slots: %d, want 64", len(gd.DropSlots))
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := importGameTables(ctx, tx, gd, by); err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM gamedata.drop_slots`, `DELETE FROM gamedata.blocked_drop_items`,
			`DELETE FROM gamedata.city_owners`, `DELETE FROM mac_bans`} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		for _, d := range gd.DropSlots {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.drop_slots (slot, base, rate) VALUES ($1, $2, $3)`, d.Slot, d.Base, d.Rate); err != nil {
				return fmt.Errorf("drop slot %d: %w", d.Slot, err)
			}
		}
		for _, id := range gd.BlockedDrops {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.blocked_drop_items (item) VALUES ($1)`, id); err != nil {
				return fmt.Errorf("blocked item %d: %w", id, err)
			}
		}
		for _, c := range gd.CityOwners {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.city_owners (channel, city, guild_id, changed_by, changed_at) VALUES ($1, $2, $3, $4, $5)`,
				c.Channel, c.City, c.GuildID, c.ChangedBy, c.ChangedAt); err != nil {
				return fmt.Errorf("city %d channel %d: %w", c.City, c.Channel, err)
			}
		}
		for _, mac := range macBans {
			if _, err := tx.Exec(ctx, `INSERT INTO mac_bans (mac) VALUES ($1)`, mac[:]); err != nil {
				return fmt.Errorf("mac ban: %w", err)
			}
		}
		if dryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		return nil
	}
	return err
}
