package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Item tracking (item_tracks / item_track_events): see 0027_item_tracks.sql.

// Event kinds, as stored.
const (
	TrackPickup      = "pickup"
	TrackNPCBuy      = "npc_buy"
	TrackNPCReward   = "npc_reward"
	TrackEventReward = "event_reward"
	TrackTrade       = "trade"
	TrackStore       = "store"
	TrackGround      = "ground"
	TrackNPCSell     = "npc_sell"
	TrackBuyback     = "buyback"
	TrackDestroyed   = "destroyed"
	TrackGone        = "gone"
	TrackChanged     = "changed" // the item or its effects changed in its holder's hands (made by DBSRV)
)

// trackChangeSQL appends a 'changed' event when the item ($2, $3) is not
// what the track has now: what it became, in the hands of its holder.
// Run it before the statement that overwrites current_item/current_effects.
const trackChangeSQL = `INSERT INTO item_track_events (track_id, at, kind, channel, from_account, from_char, to_account, to_char, item, effects)
	SELECT id, $5, 'changed', $4, COALESCE(holder_account, ''), COALESCE(holder_char, ''), COALESCE(holder_account, ''),
		COALESCE(holder_char, ''), $2, $3
	FROM item_tracks WHERE id = $1 AND (current_item <> $2 OR current_effects <> $3::jsonb)`

// ItemTrackEvent is one entry of an item's history.
type ItemTrackEvent struct {
	ID                        int64
	TrackID                   int64
	Kind                      string
	At                        time.Time
	Channel                   int
	FromAccount, FromChar     string
	ToAccount, ToChar         string
	Price                     int64
	Item                      int
	Effects                   [3]ItemEffect
	Mob                       string // creation events only (the monster or NPC), like the fields below
	DropX, DropY              int
	DroppedAt                 time.Time
	KillerAccount, KillerChar string
}

// AddItemTrackEvent appends an event and applies it to the track: a pickup
// (or an NPC's purchase or reward) creates it, a transfer changes its holder, the other kinds take it out
// of the game.
func (s *Store) AddItemTrackEvent(ctx context.Context, e ItemTrackEvent) error {
	effects, err := json.Marshal(e.Effects)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var tag string
		switch e.Kind {
		case TrackPickup, TrackNPCBuy, TrackNPCReward, TrackEventReward:
			origin := map[string]string{TrackPickup: "drop", TrackNPCBuy: "npc_buy", TrackNPCReward: "npc_reward", TrackEventReward: "event_reward"}[e.Kind]
			ct, err := tx.Exec(ctx, `INSERT INTO item_tracks (id, origin, item, effects, mob, price, channel, drop_x, drop_y, dropped_at,
					killer_account, killer_char, picked_at, holder_account, holder_char, current_item, current_effects)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $3, $4) ON CONFLICT (id) DO NOTHING`,
				e.TrackID, origin, e.Item, effects, e.Mob, e.Price, e.Channel, e.DropX, e.DropY, e.DroppedAt,
				e.KillerAccount, e.KillerChar, e.At, e.ToAccount, e.ToChar)
			if err != nil {
				return err
			}
			if ct.RowsAffected() == 0 {
				return fmt.Errorf("item track %d already exists", e.TrackID)
			}
		case TrackTrade, TrackStore, TrackGround, TrackBuyback:
			tag = "transfer"
			if _, err := tx.Exec(ctx, trackChangeSQL, e.TrackID, e.Item, effects, e.Channel, e.At); err != nil {
				return err
			}
			ct, err := tx.Exec(ctx, `UPDATE item_tracks SET status = 'active', holder_account = $2, holder_char = $3,
					holder_char_slot = NULL, loc_type = NULL, loc_slot = NULL, current_item = $4, current_effects = $5, updated_at = now()
				WHERE id = $1`, e.TrackID, e.ToAccount, e.ToChar, e.Item, effects)
			if err != nil {
				return err
			}
			if ct.RowsAffected() == 0 {
				return fmt.Errorf("item track %d not found (%s)", e.TrackID, tag)
			}
		case TrackNPCSell, TrackDestroyed, TrackGone:
			if _, err := tx.Exec(ctx, trackChangeSQL, e.TrackID, e.Item, effects, e.Channel, e.At); err != nil {
				return err
			}
			status := map[string]string{TrackNPCSell: "sold", TrackDestroyed: "destroyed", TrackGone: "gone"}[e.Kind]
			ct, err := tx.Exec(ctx, `UPDATE item_tracks SET status = $2, holder_account = NULL, holder_char = NULL,
					holder_char_slot = NULL, loc_type = NULL, loc_slot = NULL, current_item = $3, current_effects = $4, updated_at = now()
				WHERE id = $1`, e.TrackID, status, e.Item, effects)
			if err != nil {
				return err
			}
			if ct.RowsAffected() == 0 {
				return fmt.Errorf("item track %d not found (%s)", e.TrackID, e.Kind)
			}
		default:
			return fmt.Errorf("item track: unknown event %q", e.Kind)
		}
		_, err := tx.Exec(ctx, `INSERT INTO item_track_events (track_id, at, kind, channel, from_account, from_char,
				to_account, to_char, price, item, effects) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			e.TrackID, e.At, e.Kind, e.Channel, e.FromAccount, e.FromChar, e.ToAccount, e.ToChar, e.Price, e.Item, effects)
		return err
	})
}

// ItemTrackPlace is where a tracked item is inside an account. Type -1
// means it left the account's slots without changing hands yet (it is on
// the ground).
type ItemTrackPlace struct {
	TrackID  int64
	CharSlot int // -1 = storage
	Type     int
	Slot     int
	Item     int
	Effects  [3]ItemEffect
}

// SyncItemTracks records where an account's tracked items are now, and
// appends a 'changed' event for each one whose item or effects changed.
func (s *Store) SyncItemTracks(ctx context.Context, channel int, account, char string, places []ItemTrackPlace) error {
	if len(places) == 0 {
		return nil
	}
	now := time.Now()
	batch := &pgx.Batch{}
	for _, p := range places {
		effects, err := json.Marshal(p.Effects)
		if err != nil {
			return err
		}
		if p.Type < 0 {
			batch.Queue(`UPDATE item_tracks SET holder_char_slot = NULL, loc_type = NULL, loc_slot = NULL, updated_at = now()
				WHERE id = $1 AND status = 'active' AND holder_account = $2`, p.TrackID, account)
			continue
		}
		batch.Queue(trackChangeSQL, p.TrackID, p.Item, effects, channel, now)
		batch.Queue(`UPDATE item_tracks SET status = 'active', holder_account = $2, holder_char = $3, holder_char_slot = $4,
				loc_type = $5, loc_slot = $6, current_item = $7, current_effects = $8, updated_at = now()
			WHERE id = $1`, p.TrackID, account, char, p.CharSlot, p.Type, p.Slot, p.Item, effects)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return tx.SendBatch(ctx, batch).Close() })
}

// AccountItemTracks lists the active tracked items of a character and of
// its account's storage.
func (s *Store) AccountItemTracks(ctx context.Context, account string, charSlot int) ([]ItemTrackPlace, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, holder_char_slot, loc_type, loc_slot, current_item, current_effects
		FROM item_tracks WHERE status = 'active' AND holder_account = $1 AND holder_char_slot IN ($2, -1) ORDER BY id`,
		account, charSlot)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemTrackPlace, error) {
		var p ItemTrackPlace
		var raw []byte
		if err := r.Scan(&p.TrackID, &p.CharSlot, &p.Type, &p.Slot, &p.Item, &raw); err != nil {
			return p, err
		}
		return p, json.Unmarshal(raw, &p.Effects)
	})
}

// ItemTrack is a tracked item with its history.
type ItemTrack struct {
	ID                        int64
	Origin                    string // drop, npc_buy, npc_reward, event_reward
	Price                     int64
	Item                      int
	ItemName                  string
	Effects                   [3]ItemEffect
	CurrentItemName           string // FindItemTracks only
	Mob                       string
	Channel                   int
	DropX, DropY              int
	DroppedAt, PickedAt       time.Time
	KillerAccount, KillerChar string
	Status                    string
	HolderAccount, HolderChar string
	CurrentItem               int
	CurrentEffects            [3]ItemEffect
	UpdatedAt                 time.Time
	Events                    []ItemTrackEvent
}

// SearchItemTracks lists the newest tracks; q matches the track id, an
// account or a character (killer or holder), or the item number.
func (s *Store) SearchItemTracks(ctx context.Context, q string, limit int) ([]ItemTrack, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	rows, _ := s.pool.Query(ctx, `SELECT t.id, t.origin, t.price, t.item, COALESCE(i.name, ''), t.effects, t.mob, t.channel, t.drop_x, t.drop_y,
			t.dropped_at, t.picked_at, t.killer_account, t.killer_char, t.status, COALESCE(t.holder_account, ''),
			COALESCE(t.holder_char, ''), t.current_item, t.current_effects, t.updated_at
		FROM item_tracks t LEFT JOIN gamedata.items i ON i.id = t.item
		WHERE $1 = '' OR t.id::text = $1 OR t.item::text = $1 OR lower(t.killer_account) = $1 OR lower(t.killer_char) = $1
			OR lower(t.holder_account) = $1 OR lower(t.holder_char) = $1
			OR t.id IN (SELECT track_id FROM item_track_events WHERE lower(from_account) = $1 OR lower(to_account) = $1
				OR lower(from_char) = $1 OR lower(to_char) = $1)
		ORDER BY t.picked_at DESC LIMIT $2`, q, limit)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemTrack, error) {
		var t ItemTrack
		var eff, cur []byte
		if err := r.Scan(&t.ID, &t.Origin, &t.Price, &t.Item, &t.ItemName, &eff, &t.Mob, &t.Channel, &t.DropX, &t.DropY, &t.DroppedAt, &t.PickedAt,
			&t.KillerAccount, &t.KillerChar, &t.Status, &t.HolderAccount, &t.HolderChar, &t.CurrentItem, &cur, &t.UpdatedAt); err != nil {
			return t, err
		}
		_ = json.Unmarshal(eff, &t.Effects) // display only
		_ = json.Unmarshal(cur, &t.CurrentEffects)
		return t, nil
	})
}

// ItemTrackEvents is the history of one track, oldest first.
func (s *Store) ItemTrackEvents(ctx context.Context, trackID int64) ([]ItemTrackEvent, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id, track_id, kind, at, channel, from_account, from_char, to_account, to_char, price, item, effects
		FROM item_track_events WHERE track_id = $1 ORDER BY id`, trackID)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemTrackEvent, error) {
		var e ItemTrackEvent
		var raw []byte
		if err := r.Scan(&e.ID, &e.TrackID, &e.Kind, &e.At, &e.Channel, &e.FromAccount, &e.FromChar, &e.ToAccount, &e.ToChar,
			&e.Price, &e.Item, &raw); err != nil {
			return e, err
		}
		_ = json.Unmarshal(raw, &e.Effects) // display only
		return e, nil
	})
}

// ItemTrackFilter is the panel's search. The empty fields do not filter.
type ItemTrackFilter struct {
	ID      int64  // one track
	Account string // every track the account ever held (any case)
	Item    string // the item's number, or a piece of its name
	Limit   int
}

// FindItemTracks lists the tracks of a filter with their whole history,
// the ones that changed last first. An account matches the tracks it holds
// and the ones it gave or received in any event; an item matches what the
// track was created as and what it is now.
func (s *Store) FindItemTracks(ctx context.Context, f ItemTrackFilter) ([]ItemTrack, error) {
	var where []string
	var args []any
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if f.ID != 0 {
		where = append(where, "t.id = "+arg(f.ID))
	}
	if a := strings.ToUpper(strings.TrimSpace(f.Account)); a != "" {
		p := arg(a)
		where = append(where, `(t.holder_account = `+p+` OR t.id IN (SELECT track_id FROM item_track_events
			WHERE to_account = `+p+` UNION ALL SELECT track_id FROM item_track_events WHERE from_account = `+p+`))`)
	}
	if it := strings.TrimSpace(f.Item); it != "" {
		if n, err := strconv.Atoi(strings.TrimPrefix(it, "#")); err == nil {
			p := arg(n)
			where = append(where, "(t.item = "+p+" OR t.current_item = "+p+")")
		} else {
			ids := `(SELECT id FROM gamedata.items WHERE name ILIKE '%' || ` + arg(likeEscape(it)) + ` || '%')`
			where = append(where, "(t.item IN "+ids+" OR t.current_item IN "+ids+")")
		}
	}
	sql := `SELECT t.id, t.origin, t.price, t.item, COALESCE(i.name, ''), t.effects, t.mob, t.channel, t.drop_x, t.drop_y,
			t.dropped_at, t.picked_at, t.killer_account, t.killer_char, t.status, COALESCE(t.holder_account, ''),
			COALESCE(t.holder_char, ''), t.current_item, COALESCE(c.name, ''), t.current_effects, t.updated_at
		FROM item_tracks t LEFT JOIN gamedata.items i ON i.id = t.item LEFT JOIN gamedata.items c ON c.id = t.current_item`
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	sql += " ORDER BY t.updated_at DESC, t.id DESC LIMIT " + arg(f.Limit)
	rows, _ := s.pool.Query(ctx, sql, args...)
	tracks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ItemTrack, error) {
		var t ItemTrack
		var eff, cur []byte
		if err := r.Scan(&t.ID, &t.Origin, &t.Price, &t.Item, &t.ItemName, &eff, &t.Mob, &t.Channel, &t.DropX, &t.DropY, &t.DroppedAt, &t.PickedAt,
			&t.KillerAccount, &t.KillerChar, &t.Status, &t.HolderAccount, &t.HolderChar, &t.CurrentItem, &t.CurrentItemName, &cur, &t.UpdatedAt); err != nil {
			return t, err
		}
		_ = json.Unmarshal(eff, &t.Effects) // display only
		_ = json.Unmarshal(cur, &t.CurrentEffects)
		return t, nil
	})
	if err != nil || len(tracks) == 0 {
		return tracks, err
	}
	at := make(map[int64]int, len(tracks))
	ids := make([]int64, len(tracks))
	for i := range tracks {
		at[tracks[i].ID], ids[i] = i, tracks[i].ID
	}
	rows, _ = s.pool.Query(ctx, `SELECT id, track_id, kind, at, channel, from_account, from_char, to_account, to_char, price, item, effects
		FROM item_track_events WHERE track_id = ANY($1) ORDER BY id`, ids)
	var e ItemTrackEvent
	var raw []byte
	_, err = pgx.ForEachRow(rows, []any{&e.ID, &e.TrackID, &e.Kind, &e.At, &e.Channel, &e.FromAccount, &e.FromChar, &e.ToAccount, &e.ToChar,
		&e.Price, &e.Item, &raw}, func() error {
		ev := e
		ev.Effects = [3]ItemEffect{}
		_ = json.Unmarshal(raw, &ev.Effects) // display only
		t := &tracks[at[ev.TrackID]]
		t.Events = append(t.Events, ev)
		return nil
	})
	return tracks, err
}

// likeEscape makes a text literal inside a LIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
