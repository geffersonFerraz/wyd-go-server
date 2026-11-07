package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Admin panel access to the gamedata tables. Every save also writes a
// gamedata.change_log row; the notify triggers make running TMSRVs reload.

// ItemSummary is one line of the item search.
type ItemSummary struct {
	ID    int
	Name  string
	Level int
	Price int
	Pos   int
}

// Change is one gamedata.change_log row.
type Change struct {
	At      time.Time
	By      string
	Entity  string
	Key     string
	Summary string
}

func logChange(ctx context.Context, tx pgx.Tx, by, entity, key, summary string) error {
	_, err := tx.Exec(ctx, `INSERT INTO gamedata.change_log (changed_by, entity, key, summary) VALUES ($1, $2, $3, $4)`,
		by, entity, key, summary)
	return err
}

// RecentChanges lists the latest change_log rows (entity "" = all).
func (s *Store) RecentChanges(ctx context.Context, entity, key string, limit int) ([]Change, error) {
	rows, _ := s.pool.Query(ctx, `SELECT changed_at, changed_by, entity, key, summary FROM gamedata.change_log
		WHERE ($1 = '' OR entity = $1) AND ($2 = '' OR key = $2) ORDER BY id DESC LIMIT $3`, entity, key, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Change, error) {
		var c Change
		return c, r.Scan(&c.At, &c.By, &c.Entity, &c.Key, &c.Summary)
	})
}

// EffectNameList returns the effect names by id.
func (s *Store) EffectNameList(ctx context.Context) ([]EffectName, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, name FROM gamedata.effect_names ORDER BY id`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (EffectName, error) {
		var e EffectName
		return e, r.Scan(&e.ID, &e.Name)
	})
}

// SearchItems finds items by id or name (case-insensitive substring).
func (s *Store) SearchItems(ctx context.Context, q string, limit int) ([]ItemSummary, error) {
	q = strings.TrimSpace(q)
	id, _ := strconv.Atoi(q)
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
	rows, _ := s.pool.Query(ctx, `SELECT id, name, level, price, pos FROM gamedata.items
		WHERE $1 = '' OR id = $2 OR lower(name) LIKE $3 ORDER BY id LIMIT $4`, q, id, pattern, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemSummary, error) {
		var it ItemSummary
		return it, r.Scan(&it.ID, &it.Name, &it.Level, &it.Price, &it.Pos)
	})
}

// ItemNames maps ids to names for the given ids.
func (s *Store) ItemNames(ctx context.Context, ids []int) (map[int]string, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, name FROM gamedata.items WHERE id = ANY($1)`, ids)
	out := map[int]string{}
	var id int
	var name string
	_, err := pgx.ForEachRow(rows, []any{&id, &name}, func() error { out[id] = name; return nil })
	return out, err
}

// GetItem loads one item with its effects.
func (s *Store) GetItem(ctx context.Context, id int) (*Item, error) {
	var it Item
	err := s.pool.QueryRow(ctx, `SELECT id, name, mesh1, mesh2, level, req_str, req_int, req_dex, req_con,
		price, unique_type, pos, extreme, grade FROM gamedata.items WHERE id = $1`, id).Scan(&it.ID, &it.Name,
		&it.Mesh1, &it.Mesh2, &it.Level, &it.ReqStr, &it.ReqInt, &it.ReqDex, &it.ReqCon, &it.Price, &it.Unique,
		&it.Pos, &it.Extreme, &it.Grade)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT slot, effect, value FROM gamedata.item_effects WHERE item_id = $1`, id)
	var slot, eff, val int
	if _, err := pgx.ForEachRow(rows, []any{&slot, &eff, &val}, func() error {
		if slot >= 0 && slot < 12 {
			it.Effects[slot] = ItemEffect{Effect: eff, Value: val}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &it, nil
}

// SaveItem creates or replaces an item and its effects.
func (s *Store) SaveItem(ctx context.Context, it *Item, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.items (id, name, mesh1, mesh2, level, req_str, req_int, req_dex,
			req_con, price, unique_type, pos, extreme, grade) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, mesh1 = EXCLUDED.mesh1, mesh2 = EXCLUDED.mesh2,
			level = EXCLUDED.level, req_str = EXCLUDED.req_str, req_int = EXCLUDED.req_int, req_dex = EXCLUDED.req_dex,
			req_con = EXCLUDED.req_con, price = EXCLUDED.price, unique_type = EXCLUDED.unique_type, pos = EXCLUDED.pos,
			extreme = EXCLUDED.extreme, grade = EXCLUDED.grade`,
			it.ID, it.Name, it.Mesh1, it.Mesh2, it.Level, it.ReqStr, it.ReqInt, it.ReqDex, it.ReqCon, it.Price,
			it.Unique, it.Pos, it.Extreme, it.Grade); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM gamedata.item_effects WHERE item_id = $1`, it.ID); err != nil {
			return err
		}
		for slot, e := range it.Effects {
			if e.Effect == 0 && e.Value == 0 {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.item_effects (item_id, slot, effect, value) VALUES ($1,$2,$3,$4)`,
				it.ID, slot, e.Effect, e.Value); err != nil {
				return err
			}
		}
		return logChange(ctx, tx, by, "item", strconv.Itoa(it.ID), fmt.Sprintf("%s: preço %d, nível %d", it.Name, it.Price, it.Level))
	})
}

// ListSkills returns every skill.
func (s *Store) ListSkills(ctx context.Context) ([]Skill, error) {
	gd := &GameData{}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		gd.Skills, err = querySkills(ctx, tx, `ORDER BY id`)
		return err
	})
	return gd.Skills, err
}

// GetSkill loads one skill.
func (s *Store) GetSkill(ctx context.Context, id int) (*Skill, error) {
	var out []Skill
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = querySkills(ctx, tx, `WHERE id = `+strconv.Itoa(id))
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return &out[0], nil
}

func querySkills(ctx context.Context, tx pgx.Tx, tail string) ([]Skill, error) {
	rows, _ := tx.Query(ctx, `SELECT id, name, points, target, mana, delay, range, instance_type, instance_value,
		tick_type, tick_value, affect_type, affect_value, time, instance_attribute, tick_attribute, aggressive,
		max_target, party_check, affect_resist, passive, force_damage, act1, act2 FROM gamedata.skills `+tail)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Skill, error) {
		var k Skill
		return k, r.Scan(&k.ID, &k.Name, &k.Points, &k.Target, &k.Mana, &k.Delay, &k.Range, &k.InstanceType,
			&k.InstanceValue, &k.TickType, &k.TickValue, &k.AffectType, &k.AffectValue, &k.Time,
			&k.InstanceAttribute, &k.TickAttribute, &k.Aggressive, &k.MaxTarget, &k.PartyCheck,
			&k.AffectResist, &k.Passive, &k.ForceDamage, &k.Act1, &k.Act2)
	})
}

// SaveSkill creates or replaces a skill.
func (s *Store) SaveSkill(ctx context.Context, k *Skill, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.skills (id, name, points, target, mana, delay, range,
			instance_type, instance_value, tick_type, tick_value, affect_type, affect_value, time, instance_attribute,
			tick_attribute, aggressive, max_target, party_check, affect_resist, passive, force_damage, act1, act2)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, points = EXCLUDED.points, target = EXCLUDED.target,
			mana = EXCLUDED.mana, delay = EXCLUDED.delay, range = EXCLUDED.range, instance_type = EXCLUDED.instance_type,
			instance_value = EXCLUDED.instance_value, tick_type = EXCLUDED.tick_type, tick_value = EXCLUDED.tick_value,
			affect_type = EXCLUDED.affect_type, affect_value = EXCLUDED.affect_value, time = EXCLUDED.time,
			instance_attribute = EXCLUDED.instance_attribute, tick_attribute = EXCLUDED.tick_attribute,
			aggressive = EXCLUDED.aggressive, max_target = EXCLUDED.max_target, party_check = EXCLUDED.party_check,
			affect_resist = EXCLUDED.affect_resist, passive = EXCLUDED.passive, force_damage = EXCLUDED.force_damage,
			act1 = EXCLUDED.act1, act2 = EXCLUDED.act2`,
			k.ID, k.Name, k.Points, k.Target, k.Mana, k.Delay, k.Range, k.InstanceType, k.InstanceValue, k.TickType,
			k.TickValue, k.AffectType, k.AffectValue, k.Time, k.InstanceAttribute, k.TickAttribute, k.Aggressive,
			k.MaxTarget, k.PartyCheck, k.AffectResist, k.Passive, k.ForceDamage, k.Act1, k.Act2); err != nil {
			return err
		}
		return logChange(ctx, tx, by, "skill", strconv.Itoa(k.ID), fmt.Sprintf("%s: mana %d, recarga %d s", k.Name, k.Mana, k.Delay))
	})
}

// DropSummary is one line of the drop list.
type DropSummary struct {
	Name     string
	Section  string
	MaxDrops int
	Gold     int
	Lines    int
}

// SearchDrops lists drop table monsters (name substring, "" = all).
func (s *Store) SearchDrops(ctx context.Context, q string) ([]DropSummary, error) {
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(strings.TrimSpace(q))) + "%"
	rows, _ := s.pool.Query(ctx, `SELECT d.name, d.section, d.max_drops, d.gold,
		(SELECT count(*) FROM gamedata.drop_items i WHERE i.drop_name = d.name)
		FROM gamedata.drops d WHERE lower(d.name) LIKE $1 OR lower(d.section) LIKE $1 ORDER BY d.sort, d.name`, pattern)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DropSummary, error) {
		var d DropSummary
		return d, r.Scan(&d.Name, &d.Section, &d.MaxDrops, &d.Gold, &d.Lines)
	})
}

// GetDrop loads one monster's drops.
func (s *Store) GetDrop(ctx context.Context, name string) (*Drop, error) {
	d := &Drop{}
	err := s.pool.QueryRow(ctx, `SELECT name, section, max_drops, gold FROM gamedata.drops WHERE name = $1`, name).
		Scan(&d.Name, &d.Section, &d.MaxDrops, &d.Gold)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT kills, item, effect, value FROM gamedata.drop_items WHERE drop_name = $1 ORDER BY ord`, name)
	d.Items, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DropItem, error) {
		var it DropItem
		return it, r.Scan(&it.Kills, &it.Item, &it.Effect, &it.Value)
	})
	return d, err
}

// SaveDrop creates (oldName "") or updates/renames a monster's drops.
func (s *Store) SaveDrop(ctx context.Context, oldName string, d *Drop, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if oldName == "" {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.drops (name, section, max_drops, gold, sort)
				VALUES ($1, $2, $3, $4, (SELECT coalesce(max(sort), 0) + 1 FROM gamedata.drops))`,
				d.Name, d.Section, d.MaxDrops, d.Gold); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE gamedata.drops SET name = $2, section = $3, max_drops = $4, gold = $5 WHERE name = $1`,
				oldName, d.Name, d.Section, d.MaxDrops, d.Gold)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM gamedata.drop_items WHERE drop_name = $1`, d.Name); err != nil {
			return err
		}
		for ord, it := range d.Items {
			if _, err := tx.Exec(ctx, `INSERT INTO gamedata.drop_items (drop_name, ord, kills, item, effect, value)
				VALUES ($1,$2,$3,$4,$5,$6)`, d.Name, ord, it.Kills, it.Item, it.Effect, it.Value); err != nil {
				return err
			}
		}
		return logChange(ctx, tx, by, "drop", d.Name, fmt.Sprintf("%d itens, máx. %d por morte, gold %d", len(d.Items), d.MaxDrops, d.Gold))
	})
}

// DeleteDrop removes a monster from the drop table.
func (s *Store) DeleteDrop(ctx context.Context, name, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM gamedata.drops WHERE name = $1`, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return logChange(ctx, tx, by, "drop", name, "removido")
	})
}

// ListTeleports returns the teleports by id.
func (s *Store) ListTeleports(ctx context.Context) ([]Teleport, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, src_x, src_y, dst_x, dst_y, price FROM gamedata.teleports ORDER BY id`)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Teleport, error) {
		var t Teleport
		return t, r.Scan(&t.ID, &t.SrcX, &t.SrcY, &t.DstX, &t.DstY, &t.Price)
	})
}

// SaveTeleport creates or replaces a teleport.
func (s *Store) SaveTeleport(ctx context.Context, t *Teleport, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.teleports (id, src_x, src_y, dst_x, dst_y, price)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO UPDATE SET src_x = EXCLUDED.src_x, src_y = EXCLUDED.src_y,
			dst_x = EXCLUDED.dst_x, dst_y = EXCLUDED.dst_y, price = EXCLUDED.price`,
			t.ID, t.SrcX, t.SrcY, t.DstX, t.DstY, t.Price); err != nil {
			return err
		}
		return logChange(ctx, tx, by, "teleport", strconv.Itoa(t.ID),
			fmt.Sprintf("%dx%d -> %dx%d, %d gold", t.SrcX, t.SrcY, t.DstX, t.DstY, t.Price))
	})
}

// DeleteTeleport removes a teleport.
func (s *Store) DeleteTeleport(ctx context.Context, id int, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM gamedata.teleports WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return logChange(ctx, tx, by, "teleport", strconv.Itoa(id), "removido")
	})
}
