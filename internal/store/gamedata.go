package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GameData is the gamedata schema: the static tables the TMSRV loads.
type GameData struct {
	EffectNames  []EffectName
	Items        []Item
	Skills       []Skill
	Drops        []Drop
	DropSlots    []DropSlot
	BlockedDrops []int
	Teleports    []Teleport
	// CityOwners are the cities the admin panel gave to a guild (cities
	// without a row keep guild_zone.txt's owner).
	CityOwners []CityOwner
}

type EffectName struct {
	ID   int
	Name string
}

// Item is one gamedata.items row with its 12 effect pairs (zero pairs are
// not stored).
type Item struct {
	ID                             int
	Name                           string
	Mesh1                          int
	Mesh2                          int
	Level                          int
	ReqStr, ReqInt, ReqDex, ReqCon int
	Price                          int
	Unique                         int
	Pos                            int
	Extreme                        int
	Grade                          int
	Effects                        [12]ItemEffect
}

type ItemEffect struct {
	Effect int
	Value  int
}

// Skill is one gamedata.skills row.
type Skill struct {
	ID                                           int
	Name                                         string
	Points, Target, Mana, Delay, Range           int
	InstanceType, InstanceValue                  int
	TickType, TickValue, AffectType, AffectValue int
	Time, InstanceAttribute, TickAttribute       int
	Aggressive, MaxTarget, PartyCheck            int
	AffectResist, Passive, ForceDamage           int
	Act1, Act2                                   string
}

// Drop is one monster of the drop table.
type Drop struct {
	Name     string
	Section  string
	MaxDrops int
	Gold     int
	RateGold int64 // gold drawn by chance when the monster dies
	Items    []DropItem
	Rate     []RateDrop
}

// RateDrop is a drop by chance, in one slot of the monster's list.
type RateDrop struct {
	Slot    int
	Item    int
	Effects [3]ItemEffect
	Chance  int // 1 in Chance; 0 = the slot's base and rate
}

// DropSlot is the chance settings of one inventory slot.
type DropSlot struct {
	Slot, Base, Rate int
}

type DropItem struct {
	Kills  int
	Item   int
	Effect int
	Value  int
}

type Teleport struct {
	ID                     int
	SrcX, SrcY, DstX, DstY int
	Price                  int
}

// ImportGameData replaces every gamedata table with gd in one transaction.
func (s *Store) ImportGameData(ctx context.Context, gd *GameData, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return importGameTables(ctx, tx, gd, by) })
}

func importGameTables(ctx context.Context, tx pgx.Tx, gd *GameData, by string) error {
	{
		if _, err := tx.Exec(ctx, `TRUNCATE gamedata.rate_drops, gamedata.drop_items, gamedata.drops, gamedata.item_effects,
			gamedata.items, gamedata.skills, gamedata.teleports, gamedata.effect_names`); err != nil {
			return err
		}
		copyRows := func(table string, cols []string, n int, row func(i int) []any) error {
			_, err := tx.CopyFrom(ctx, pgx.Identifier{"gamedata", table}, cols, pgx.CopyFromSlice(n, func(i int) ([]any, error) { return row(i), nil }))
			if err != nil {
				return fmt.Errorf("gamedata.%s: %w", table, err)
			}
			return nil
		}
		if err := copyRows("effect_names", []string{"id", "name"}, len(gd.EffectNames), func(i int) []any {
			e := gd.EffectNames[i]
			return []any{e.ID, e.Name}
		}); err != nil {
			return err
		}
		if err := copyRows("items", []string{"id", "name", "mesh1", "mesh2", "level", "req_str", "req_int", "req_dex", "req_con",
			"price", "unique_type", "pos", "extreme", "grade"}, len(gd.Items), func(i int) []any {
			it := gd.Items[i]
			return []any{it.ID, it.Name, it.Mesh1, it.Mesh2, it.Level, it.ReqStr, it.ReqInt, it.ReqDex, it.ReqCon,
				it.Price, it.Unique, it.Pos, it.Extreme, it.Grade}
		}); err != nil {
			return err
		}
		var effects [][]any
		for _, it := range gd.Items {
			for slot, e := range it.Effects {
				if e.Effect != 0 || e.Value != 0 {
					effects = append(effects, []any{it.ID, slot, e.Effect, e.Value})
				}
			}
		}
		if err := copyRows("item_effects", []string{"item_id", "slot", "effect", "value"}, len(effects), func(i int) []any { return effects[i] }); err != nil {
			return err
		}
		if err := copyRows("skills", []string{"id", "name", "points", "target", "mana", "delay", "range", "instance_type",
			"instance_value", "tick_type", "tick_value", "affect_type", "affect_value", "time", "instance_attribute",
			"tick_attribute", "aggressive", "max_target", "party_check", "affect_resist", "passive", "force_damage",
			"act1", "act2"}, len(gd.Skills), func(i int) []any {
			k := gd.Skills[i]
			return []any{k.ID, k.Name, k.Points, k.Target, k.Mana, k.Delay, k.Range, k.InstanceType, k.InstanceValue,
				k.TickType, k.TickValue, k.AffectType, k.AffectValue, k.Time, k.InstanceAttribute, k.TickAttribute,
				k.Aggressive, k.MaxTarget, k.PartyCheck, k.AffectResist, k.Passive, k.ForceDamage, k.Act1, k.Act2}
		}); err != nil {
			return err
		}
		if err := copyRows("drops", []string{"name", "section", "max_drops", "gold", "rate_gold", "sort"}, len(gd.Drops), func(i int) []any {
			d := gd.Drops[i]
			return []any{d.Name, d.Section, d.MaxDrops, d.Gold, d.RateGold, i}
		}); err != nil {
			return err
		}
		var dropItems [][]any
		for _, d := range gd.Drops {
			for ord, it := range d.Items {
				dropItems = append(dropItems, []any{d.Name, ord, it.Kills, it.Item, it.Effect, it.Value})
			}
		}
		if err := copyRows("drop_items", []string{"drop_name", "ord", "kills", "item", "effect", "value"}, len(dropItems), func(i int) []any { return dropItems[i] }); err != nil {
			return err
		}
		var rateRows [][]any
		for _, d := range gd.Drops {
			for _, r := range d.Rate {
				rateRows = append(rateRows, rateDropRow(d.Name, r))
			}
		}
		if err := copyRows("rate_drops", rateDropCols, len(rateRows), func(i int) []any { return rateRows[i] }); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.rate_drop_import (imported_by) VALUES ($1) ON CONFLICT (id) DO NOTHING`, by); err != nil {
			return err
		}
		if err := copyRows("teleports", []string{"id", "src_x", "src_y", "dst_x", "dst_y", "price"}, len(gd.Teleports), func(i int) []any {
			t := gd.Teleports[i]
			return []any{t.ID, t.SrcX, t.SrcY, t.DstX, t.DstY, t.Price}
		}); err != nil {
			return err
		}
		return logChange(ctx, tx, by, "import", "all", fmt.Sprintf("%d itens, %d skills, %d drops, %d teleportes",
			len(gd.Items), len(gd.Skills), len(gd.Drops), len(gd.Teleports)))
	}
}

// LoadGameData reads every gamedata table in one consistent snapshot.
func (s *Store) LoadGameData(ctx context.Context) (*GameData, error) {
	gd := &GameData{}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		rows, _ := tx.Query(ctx, `SELECT id, name FROM gamedata.effect_names ORDER BY id`)
		names, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (EffectName, error) {
			var e EffectName
			return e, r.Scan(&e.ID, &e.Name)
		})
		if err != nil {
			return fmt.Errorf("effect_names: %w", err)
		}
		gd.EffectNames = names

		rows, _ = tx.Query(ctx, `SELECT id, name, mesh1, mesh2, level, req_str, req_int, req_dex, req_con,
			price, unique_type, pos, extreme, grade FROM gamedata.items ORDER BY id`)
		items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Item, error) {
			var it Item
			return it, r.Scan(&it.ID, &it.Name, &it.Mesh1, &it.Mesh2, &it.Level, &it.ReqStr, &it.ReqInt, &it.ReqDex,
				&it.ReqCon, &it.Price, &it.Unique, &it.Pos, &it.Extreme, &it.Grade)
		})
		if err != nil {
			return fmt.Errorf("items: %w", err)
		}
		byID := make(map[int]int, len(items))
		for i, it := range items {
			byID[it.ID] = i
		}
		rows, _ = tx.Query(ctx, `SELECT item_id, slot, effect, value FROM gamedata.item_effects`)
		var id, slot, eff, val int
		if _, err := pgx.ForEachRow(rows, []any{&id, &slot, &eff, &val}, func() error {
			if i, ok := byID[id]; ok && slot >= 0 && slot < 12 {
				items[i].Effects[slot] = ItemEffect{Effect: eff, Value: val}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("item_effects: %w", err)
		}
		gd.Items = items

		rows, _ = tx.Query(ctx, `SELECT id, name, points, target, mana, delay, range, instance_type, instance_value,
			tick_type, tick_value, affect_type, affect_value, time, instance_attribute, tick_attribute, aggressive,
			max_target, party_check, affect_resist, passive, force_damage, act1, act2 FROM gamedata.skills ORDER BY id`)
		skills, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Skill, error) {
			var k Skill
			return k, r.Scan(&k.ID, &k.Name, &k.Points, &k.Target, &k.Mana, &k.Delay, &k.Range, &k.InstanceType,
				&k.InstanceValue, &k.TickType, &k.TickValue, &k.AffectType, &k.AffectValue, &k.Time,
				&k.InstanceAttribute, &k.TickAttribute, &k.Aggressive, &k.MaxTarget, &k.PartyCheck,
				&k.AffectResist, &k.Passive, &k.ForceDamage, &k.Act1, &k.Act2)
		})
		if err != nil {
			return fmt.Errorf("skills: %w", err)
		}
		gd.Skills = skills

		rows, _ = tx.Query(ctx, `SELECT name, section, max_drops, gold, rate_gold FROM gamedata.drops ORDER BY sort, name`)
		drops, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Drop, error) {
			var d Drop
			return d, r.Scan(&d.Name, &d.Section, &d.MaxDrops, &d.Gold, &d.RateGold)
		})
		if err != nil {
			return fmt.Errorf("drops: %w", err)
		}
		dropAt := make(map[string]int, len(drops))
		for i, d := range drops {
			dropAt[d.Name] = i
		}
		rows, _ = tx.Query(ctx, `SELECT drop_name, kills, item, effect, value FROM gamedata.drop_items ORDER BY drop_name, ord`)
		var dn string
		var di DropItem
		if _, err := pgx.ForEachRow(rows, []any{&dn, &di.Kills, &di.Item, &di.Effect, &di.Value}, func() error {
			if i, ok := dropAt[dn]; ok {
				drops[i].Items = append(drops[i].Items, di)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("drop_items: %w", err)
		}
		rows, _ = tx.Query(ctx, `SELECT `+rateDropSelect+` FROM gamedata.rate_drops ORDER BY drop_name, slot`)
		var rn string
		var rd RateDrop
		if _, err := pgx.ForEachRow(rows, append([]any{&rn}, rateDropScan(&rd)...), func() error {
			if i, ok := dropAt[rn]; ok {
				drops[i].Rate = append(drops[i].Rate, rd)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("rate_drops: %w", err)
		}
		gd.Drops = drops

		rows, _ = tx.Query(ctx, `SELECT slot, base, rate FROM gamedata.drop_slots ORDER BY slot`)
		if gd.DropSlots, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DropSlot, error) {
			var ds DropSlot
			return ds, r.Scan(&ds.Slot, &ds.Base, &ds.Rate)
		}); err != nil {
			return fmt.Errorf("drop_slots: %w", err)
		}
		rows, _ = tx.Query(ctx, `SELECT item FROM gamedata.blocked_drop_items ORDER BY item`)
		if gd.BlockedDrops, err = pgx.CollectRows(rows, pgx.RowTo[int]); err != nil {
			return fmt.Errorf("blocked_drop_items: %w", err)
		}

		rows, _ = tx.Query(ctx, `SELECT id, src_x, src_y, dst_x, dst_y, price FROM gamedata.teleports ORDER BY id`)
		tps, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Teleport, error) {
			var t Teleport
			return t, r.Scan(&t.ID, &t.SrcX, &t.SrcY, &t.DstX, &t.DstY, &t.Price)
		})
		if err != nil {
			return fmt.Errorf("teleports: %w", err)
		}
		gd.Teleports = tps

		rows, _ = tx.Query(ctx, `SELECT channel, city, guild_id, changed_by, changed_at FROM gamedata.city_owners ORDER BY channel, city`)
		owners, err := pgx.CollectRows(rows, scanCityOwner)
		if err != nil {
			return fmt.Errorf("city_owners: %w", err)
		}
		if len(owners) > 0 {
			gd.CityOwners = owners
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return gd, nil
}
