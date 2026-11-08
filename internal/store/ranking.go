package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"dbsrv/internal/wire"
)

// RankEntry is one character in the shared ranking.
type RankEntry struct {
	Name                                   string
	Level, Class, Evolution, Guild, Patent int
	Kingdom, Frags, Power, Fame, Citizen   int
	PvPKill, PvMKill, Mantle               int
	Exp                                    int64
	Equip                                  [20]wire.Item
}

// SaveRank stores a character's current ranking row.
func (s *Store) SaveRank(ctx context.Context, e RankEntry) error {
	equip, err := wire.Marshal(e.Equip)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO ranking (name_key, name, level, class_info, evolution, guild_index, patent, kingdom, frags, power,
			fame, citizen, pvp_kill, pvm_kill, mantle, exp, equip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (name_key) DO UPDATE SET name = $2, level = $3, class_info = $4, evolution = $5, guild_index = $6,
			patent = $7, kingdom = $8, frags = $9, power = $10, fame = $11, citizen = $12, pvp_kill = $13, pvm_kill = $14,
			mantle = $15, exp = $16, equip = $17, updated_at = now()`,
		strings.ToLower(e.Name), e.Name, e.Level, e.Class, e.Evolution, e.Guild, e.Patent, e.Kingdom, e.Frags, e.Power,
		e.Fame, e.Citizen, e.PvPKill, e.PvMKill, e.Mantle, e.Exp, equip)
	return err
}

// Ranking lists the ranked characters that still exist.
func (s *Store) Ranking(ctx context.Context) ([]RankEntry, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT r.name, r.level, r.class_info, r.evolution, r.guild_index, r.patent, r.kingdom, r.frags, r.power,
			r.fame, r.citizen, r.pvp_kill, r.pvm_kill, r.mantle, r.exp, r.equip
		FROM ranking r WHERE EXISTS (SELECT 1 FROM characters c WHERE lower(c.name) = r.name_key)`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RankEntry, error) {
		var e RankEntry
		var equip []byte
		if err := r.Scan(&e.Name, &e.Level, &e.Class, &e.Evolution, &e.Guild, &e.Patent, &e.Kingdom, &e.Frags, &e.Power,
			&e.Fame, &e.Citizen, &e.PvPKill, &e.PvMKill, &e.Mantle, &e.Exp, &equip); err != nil {
			return e, err
		}
		if err := wire.Unmarshal(equip, &e.Equip); err != nil {
			return e, fmt.Errorf("ranking %s: %w", e.Name, err)
		}
		return e, nil
	})
}
