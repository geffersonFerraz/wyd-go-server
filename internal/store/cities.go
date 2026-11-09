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
	Channel   int
	City      int
	GuildID   int
	ChangedBy string
	ChangedAt time.Time
}

// MaxCity is the number of cities (the cities section of tmsrv.yaml).
const MaxCity = 5

func scanCityOwner(r pgx.CollectableRow) (CityOwner, error) {
	var c CityOwner
	return c, r.Scan(&c.Channel, &c.City, &c.GuildID, &c.ChangedBy, &c.ChangedAt)
}

// CityOwners lists the cities set in the panel.
func (s *Store) CityOwners(ctx context.Context) ([]CityOwner, error) {
	rows, _ := s.pool.Query(ctx, `SELECT channel, city, guild_id, changed_by, changed_at FROM gamedata.city_owners ORDER BY channel, city`)
	return pgx.CollectRows(rows, scanCityOwner)
}

// MaxChannel is the highest channel number.
const MaxChannel = 10

// SeedCityOwners adds, on every channel, the cities that have no row yet
// (existing rows are kept). It returns how many were added.
func (s *Store) SeedCityOwners(ctx context.Context, owners map[int]int, by string) (int, error) {
	added := 0
	for city, guild := range owners {
		if city < 0 || city >= MaxCity || guild < 0 || guild >= 65535 {
			continue
		}
		for channel := 1; channel <= MaxChannel; channel++ {
			tag, err := s.pool.Exec(ctx, `INSERT INTO gamedata.city_owners (channel, city, guild_id, changed_by) VALUES ($1, $2, $3, $4)
				ON CONFLICT (channel, city) DO NOTHING`, channel, city, guild, by)
			if err != nil {
				return added, err
			}
			added += int(tag.RowsAffected())
		}
	}
	return added, nil
}

// SetCityOwner gives a channel's city to guildID (0 = no owner); that TMSRV
// picks it up through the gamedata notification.
func (s *Store) SetCityOwner(ctx context.Context, channel, city, guildID int, by string) error {
	if channel < 1 || channel > MaxChannel || city < 0 || city >= MaxCity || guildID < 0 || guildID >= 65535 {
		return fmt.Errorf("channel %d / city %d / guild %d out of range", channel, city, guildID)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO gamedata.city_owners (channel, city, guild_id, changed_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel, city) DO UPDATE SET guild_id = EXCLUDED.guild_id, changed_by = EXCLUDED.changed_by, changed_at = now()`,
		channel, city, guildID, by)
	return err
}
