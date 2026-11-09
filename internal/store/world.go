package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"dbsrv/internal/wire"
)

// Guild is a guilds row (STRUCT_GUILDINFO).
type Guild struct {
	ID       int
	Name     string
	Kingdom  int
	Fame     int
	Citizen  int
	Wins     int
	SubGuild [3]string
}

func (s *Store) UpsertGuild(ctx context.Context, g Guild) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO guilds (id, name, kingdom, fame, citizen, wins, sub1, sub2, sub3)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, kingdom = EXCLUDED.kingdom,
			fame = EXCLUDED.fame, citizen = EXCLUDED.citizen, wins = EXCLUDED.wins,
			sub1 = EXCLUDED.sub1, sub2 = EXCLUDED.sub2, sub3 = EXCLUDED.sub3`,
		g.ID, g.Name, g.Kingdom, g.Fame, g.Citizen, g.Wins, g.SubGuild[0], g.SubGuild[1], g.SubGuild[2])
	return err
}

func (s *Store) Guilds(ctx context.Context) ([]Guild, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, name, kingdom, fame, citizen, wins, sub1, sub2, sub3 FROM guilds ORDER BY id`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Guild, error) {
		var g Guild
		err := r.Scan(&g.ID, &g.Name, &g.Kingdom, &g.Fame, &g.Citizen, &g.Wins, &g.SubGuild[0], &g.SubGuild[1], &g.SubGuild[2])
		return g, err
	})
}

// DonateSlot is a donate_store row (STRUCT_STOREDONATE).
type DonateSlot struct {
	Store, Slot int
	Price       int64
	Loop        int
	Available   int
	Item        wire.Item
}

// ReplaceDonateStore swaps the whole store contents atomically.
func (s *Store) ReplaceDonateStore(ctx context.Context, slots []DonateSlot) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM donate_store`); err != nil {
			return err
		}
		for _, d := range slots {
			e := d.Item.Effect
			if _, err := tx.Exec(ctx, `
				INSERT INTO donate_store (store, slot, price, loop, available, item_id, ef1, efv1, ef2, efv2, ef3, efv3)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				d.Store, d.Slot, d.Price, d.Loop, d.Available, int32(d.Item.Index),
				int16(e[0].Index), int32(e[0].Value), int16(e[1].Index), int32(e[1].Value), int16(e[2].Index), int32(e[2].Value)); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpsertSeal stores a sealed character and moves seal_id_seq past its id.
func (s *Store) UpsertSeal(ctx context.Context, id int, info wire.SealInfo, char wire.CharInfo) error {
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return err
	}
	charJSON, err := compactJSON(&char)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO seals (id, info, character) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE SET info = EXCLUDED.info, character = EXCLUDED.character`,
			id, infoJSON, charJSON); err != nil {
			return err
		}
		return s.bumpSealSeq(ctx, tx, id)
	})
}

// AdvanceSealSeq makes the next seal id at least lastSeal+1 (Config.txt LastSeal).
func (s *Store) AdvanceSealSeq(ctx context.Context, lastSeal int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return s.bumpSealSeq(ctx, tx, lastSeal) })
}

func (s *Store) bumpSealSeq(ctx context.Context, tx pgx.Tx, atLeast int) error {
	if atLeast < 1 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		SELECT setval('seal_id_seq', GREATEST($1::bigint,
			(SELECT CASE WHEN is_called THEN last_value ELSE last_value - 1 END FROM seal_id_seq)), true)`, atLeast)
	return err
}

func (s *Store) Sapphire(ctx context.Context) (int, error) {
	var v int
	err := s.pool.QueryRow(ctx, `SELECT sapphire FROM server_state WHERE id = 1`).Scan(&v)
	return v, err
}

func (s *Store) SetSapphire(ctx context.Context, v int) error {
	_, err := s.pool.Exec(ctx, `UPDATE server_state SET sapphire = $1 WHERE id = 1`, v)
	return err
}
