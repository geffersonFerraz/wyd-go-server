package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CityOwner is one gamedata.city_owners row: the guild that holds a city
// (0 = nobody), set in the admin panel.
type CityOwner struct {
	City      int
	GuildID   int
	ChangedBy string
	ChangedAt time.Time
}

// MaxCity is the number of cities (guild_zone.txt rows 0..4).
const MaxCity = 5

func scanCityOwner(r pgx.CollectableRow) (CityOwner, error) {
	var c CityOwner
	return c, r.Scan(&c.City, &c.GuildID, &c.ChangedBy, &c.ChangedAt)
}

// CityOwners lists the cities set in the panel.
func (s *Store) CityOwners(ctx context.Context) ([]CityOwner, error) {
	rows, _ := s.pool.Query(ctx, `SELECT city, guild_id, changed_by, changed_at FROM gamedata.city_owners ORDER BY city`)
	return pgx.CollectRows(rows, scanCityOwner)
}

// SeedCityOwners adds the cities that have no row yet (existing rows are
// kept). It returns how many were added.
func (s *Store) SeedCityOwners(ctx context.Context, owners map[int]int, by string) (int, error) {
	added := 0
	for city, guild := range owners {
		if city < 0 || city >= MaxCity || guild < 0 || guild >= 65535 {
			continue
		}
		tag, err := s.pool.Exec(ctx, `INSERT INTO gamedata.city_owners (city, guild_id, changed_by) VALUES ($1, $2, $3)
			ON CONFLICT (city) DO NOTHING`, city, guild, by)
		if err != nil {
			return added, err
		}
		added += int(tag.RowsAffected())
	}
	return added, nil
}

// SetCityOwner gives city to guildID (0 = no owner) on every channel; the
// TMSRVs pick it up through the gamedata notification.
func (s *Store) SetCityOwner(ctx context.Context, city, guildID int, by string) error {
	if city < 0 || city >= MaxCity || guildID < 0 || guildID >= 65535 {
		return fmt.Errorf("city %d / guild %d out of range", city, guildID)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO gamedata.city_owners (city, guild_id, changed_by) VALUES ($1, $2, $3)
		ON CONFLICT (city) DO UPDATE SET guild_id = EXCLUDED.guild_id, changed_by = EXCLUDED.changed_by, changed_at = now()`,
		city, guildID, by)
	return err
}
