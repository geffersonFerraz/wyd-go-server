package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Evolutions of a character, as the game numbers them (Equip[0] effect 2).
const (
	EvoMortal    = 1
	EvoArch      = 2
	EvoCelestial = 3
	EvoSubCele   = 4
)

// Base experience modes of ExpRule.
const (
	ExpBaseTemplate = "template" // the Exp of the monster's template
	ExpBaseLevel    = "level"    // BaseFixed + BasePerLevel x the monster's level
)

// ExpEvolution is what one evolution gets.
type ExpEvolution struct {
	// Percent of the experience this evolution keeps (100 = all).
	Percent int
	// LevelOffset is added to the character's level before it is compared
	// with the monster's (a Celestial restarts at level 1).
	LevelOffset int
}

// ExpRule is gamedata.exp_rule: how the experience of a kill is calculated.
//
//	exp = base x level% x evolution% x global%
//
// level% depends on the difference between the monster's level and the
// character's (plus its evolution's LevelOffset):
//   - within EqualRange levels: EqualPercent;
//   - character below the monster: EqualPercent + LowerPerLevel for each
//     level beyond the range, up to LowerMaxPercent;
//   - character above the monster: EqualPercent - HigherPerLevel for each
//     level beyond the range, down to HigherMinPercent.
type ExpRule struct {
	// Enabled false keeps the C++ calculation (GetExpApply and the zone table).
	Enabled bool
	// GlobalPercent multiplies every kill (100 = normal, 200 = double).
	GlobalPercent int

	BaseMode     string
	BaseFixed    int64
	BasePerLevel int64

	EqualRange       int
	EqualPercent     int
	LowerPerLevel    int
	LowerMaxPercent  int
	HigherPerLevel   int
	HigherMinPercent int

	// Evolutions by evolution number; index 0 is not used.
	Evolutions [5]ExpEvolution

	// ZoneTable also applies the zone and evolution table of the C++
	// (exp_bonus of tmsrv.yaml), which fixes the experience outside the
	// hunting zones.
	ZoneTable bool
}

// DefaultExpRule is the rule of a database that has none: disabled, with
// values the staff starts from.
func DefaultExpRule() ExpRule {
	r := ExpRule{GlobalPercent: 100, BaseMode: ExpBaseTemplate, BasePerLevel: 100,
		EqualRange: 2, EqualPercent: 100, LowerPerLevel: 5, LowerMaxPercent: 200, HigherPerLevel: 5, HigherMinPercent: 10}
	r.Evolutions = [5]ExpEvolution{{Percent: 100}, {Percent: 100}, {Percent: 70}, {Percent: 40}, {Percent: 30}}
	return r
}

// Check validates the rule.
func (r *ExpRule) Check() error {
	in := func(name string, v, lo, hi int) error {
		if v < lo || v > hi {
			return fmt.Errorf("exp rule: %s must be %d..%d, got %d", name, lo, hi, v)
		}
		return nil
	}
	if r.BaseMode != ExpBaseTemplate && r.BaseMode != ExpBaseLevel {
		return fmt.Errorf("exp rule: unknown base mode %q", r.BaseMode)
	}
	if r.BaseFixed < 0 || r.BaseFixed > 2_000_000_000 || r.BasePerLevel < 0 || r.BasePerLevel > 2_000_000_000 {
		return errors.New("exp rule: base values must be 0..2000000000")
	}
	for _, c := range []struct {
		name      string
		v, lo, hi int
	}{
		{"GlobalPercent", r.GlobalPercent, 0, 100000}, {"EqualRange", r.EqualRange, 0, 400},
		{"EqualPercent", r.EqualPercent, 0, 10000}, {"LowerPerLevel", r.LowerPerLevel, 0, 10000},
		{"LowerMaxPercent", r.LowerMaxPercent, 0, 100000}, {"HigherPerLevel", r.HigherPerLevel, 0, 10000},
		{"HigherMinPercent", r.HigherMinPercent, 0, 10000},
	} {
		if err := in(c.name, c.v, c.lo, c.hi); err != nil {
			return err
		}
	}
	if r.LowerMaxPercent < r.EqualPercent {
		return errors.New("exp rule: LowerMaxPercent is below EqualPercent")
	}
	if r.HigherMinPercent > r.EqualPercent {
		return errors.New("exp rule: HigherMinPercent is above EqualPercent")
	}
	for ev, e := range r.Evolutions {
		if err := in(fmt.Sprintf("Evolutions[%d].Percent", ev), e.Percent, 0, 10000); err != nil {
			return err
		}
		if err := in(fmt.Sprintf("Evolutions[%d].LevelOffset", ev), e.LevelOffset, 0, 1000); err != nil {
			return err
		}
	}
	return nil
}

func (r *ExpRule) evolution(ev int) ExpEvolution {
	if ev <= 0 || ev >= len(r.Evolutions) {
		return ExpEvolution{Percent: 100}
	}
	return r.Evolutions[ev]
}

// Base is the experience of a monster before the multipliers: templateExp is
// the Exp of its template and mobLevel the level the game shows (from 1).
func (r *ExpRule) Base(templateExp int64, mobLevel int) int64 {
	if r.BaseMode == ExpBaseLevel {
		return r.BaseFixed + r.BasePerLevel*int64(max(mobLevel, 0))
	}
	return templateExp
}

// LevelPercent is the share for a character of charLevel and evolution ev
// killing a monster of mobLevel.
func (r *ExpRule) LevelPercent(charLevel, mobLevel, ev int) int {
	diff := mobLevel - (charLevel + r.evolution(ev).LevelOffset) // > 0: the character is below
	switch {
	case diff > r.EqualRange:
		return min(r.EqualPercent+(diff-r.EqualRange)*r.LowerPerLevel, r.LowerMaxPercent)
	case diff < -r.EqualRange:
		return max(r.EqualPercent-(-diff-r.EqualRange)*r.HigherPerLevel, r.HigherMinPercent)
	}
	return r.EqualPercent
}

// Apply is the experience of one kill, before what the character itself
// adds (items, experience chest, fairy).
func (r *ExpRule) Apply(templateExp int64, charLevel, mobLevel, ev int) int64 {
	exp := r.Base(templateExp, mobLevel)
	exp = exp * int64(r.LevelPercent(charLevel, mobLevel, ev)) / 100
	exp = exp * int64(r.evolution(ev).Percent) / 100
	return max(exp*int64(r.GlobalPercent)/100, 0)
}

func loadExpRule(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (*ExpRule, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT settings FROM gamedata.exp_rule`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := DefaultExpRule() // a field added later keeps its default
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("exp_rule: %w", err)
	}
	return &r, r.Check()
}

func saveExpRule(ctx context.Context, tx pgx.Tx, r *ExpRule) error {
	if err := r.Check(); err != nil {
		return err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO gamedata.exp_rule (settings) VALUES ($1::jsonb)
		ON CONFLICT (id) DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()`, string(raw))
	return err
}

// ExpRule reads the experience rule (the default, disabled, if none was saved).
func (s *Store) ExpRule(ctx context.Context) (ExpRule, error) {
	r, err := loadExpRule(ctx, s.pool)
	if err != nil {
		return ExpRule{}, err
	}
	if r == nil {
		return DefaultExpRule(), nil
	}
	return *r, nil
}

// SaveExpRule stores the experience rule.
func (s *Store) SaveExpRule(ctx context.Context, r *ExpRule, by, summary string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := saveExpRule(ctx, tx, r); err != nil {
			return err
		}
		return logChange(ctx, tx, by, "exp", "regra", summary)
	})
}
