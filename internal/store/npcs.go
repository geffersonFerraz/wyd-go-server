package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// NPC generators (gamedata.npc_generators) and the panel's access to the NPC
// templates (gamedata.mob_templates).

// MaxGenerators is MAX_NPCGENERATOR.
const MaxGenerators = 8192

// Generator is one gamedata.npc_generators row (STRUCT_NPCGENERATOR). Leader
// and Follower are names of templates of kind npc.
type Generator struct {
	ID           int
	Minute       int
	MaxMobs      int
	MinGroup     int
	MaxGroup     int
	Formation    int
	Route        int
	Leader       string
	Follower     string
	Segments     []GeneratorSegment
	FightActions []string
	DieActions   []string
}

// GeneratorSegment is a point of the route: 0 the start, 4 the destination.
type GeneratorSegment struct {
	Segment int
	X, Y    int
	Range   int
	Wait    int
	Action  string
}

// Check validates one generator.
func (g *Generator) Check() error {
	if g.ID < 0 || g.ID >= MaxGenerators {
		return fmt.Errorf("generator id must be 0..%d, got %d", MaxGenerators-1, g.ID)
	}
	if len(g.FightActions) > 4 || len(g.DieActions) > 4 {
		return fmt.Errorf("generator %d: at most 4 fight and die actions", g.ID)
	}
	seen := map[int]bool{}
	for _, s := range g.Segments {
		if s.Segment < 0 || s.Segment > 4 || seen[s.Segment] {
			return fmt.Errorf("generator %d: bad or repeated segment %d", g.ID, s.Segment)
		}
		seen[s.Segment] = true
	}
	return nil
}

const generatorCols = `id, minute, max_mobs, min_group, max_group, formation, route, leader, follower, segments, fight_actions, die_actions`

func scanGenerator(r pgx.CollectableRow) (Generator, error) {
	var g Generator
	var segs []byte
	if err := r.Scan(&g.ID, &g.Minute, &g.MaxMobs, &g.MinGroup, &g.MaxGroup, &g.Formation, &g.Route, &g.Leader, &g.Follower,
		&segs, &g.FightActions, &g.DieActions); err != nil {
		return g, err
	}
	if err := json.Unmarshal(segs, &g.Segments); err != nil {
		return g, fmt.Errorf("generator %d: segments: %w", g.ID, err)
	}
	return g, nil
}

func generatorRow(g *Generator) ([]any, error) {
	segs := g.Segments
	if segs == nil {
		segs = []GeneratorSegment{}
	}
	raw, err := json.Marshal(segs)
	if err != nil {
		return nil, err
	}
	fight, die := g.FightActions, g.DieActions
	if fight == nil {
		fight = []string{}
	}
	if die == nil {
		die = []string{}
	}
	return []any{g.ID, g.Minute, g.MaxMobs, g.MinGroup, g.MaxGroup, g.Formation, g.Route, g.Leader, g.Follower, string(raw), fight, die}, nil
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadGenerators(ctx context.Context, q querier) ([]Generator, error) {
	rows, _ := q.Query(ctx, `SELECT `+generatorCols+` FROM gamedata.npc_generators ORDER BY id`)
	return pgx.CollectRows(rows, scanGenerator)
}

// Generators reads every generator, by id.
func (s *Store) Generators(ctx context.Context) ([]Generator, error) {
	return loadGenerators(ctx, s.pool)
}

// replaceGenerators makes the table equal to gens.
func replaceGenerators(ctx context.Context, tx pgx.Tx, gens []Generator) error {
	if _, err := tx.Exec(ctx, `DELETE FROM gamedata.npc_generators`); err != nil {
		return err
	}
	return insertGenerators(ctx, tx, gens)
}

func insertGenerators(ctx context.Context, tx pgx.Tx, gens []Generator) error {
	b := &pgx.Batch{}
	for i := range gens {
		if err := gens[i].Check(); err != nil {
			return err
		}
		row, err := generatorRow(&gens[i])
		if err != nil {
			return err
		}
		b.Queue(`INSERT INTO gamedata.npc_generators (`+generatorCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`, row...)
	}
	if b.Len() == 0 {
		return nil
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("gamedata.npc_generators: %w", err)
	}
	return nil
}

// SeedGenerators fills an empty table (the first start after the generators
// left config/npc_generators.yaml). It returns how many rows it wrote.
func (s *Store) SeedGenerators(ctx context.Context, gens []Generator, by string) (int, error) {
	n := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE gamedata.npc_generators IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		var have int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM gamedata.npc_generators`).Scan(&have); err != nil || have > 0 {
			return err
		}
		if err := insertGenerators(ctx, tx, gens); err != nil {
			return err
		}
		n = len(gens)
		if n == 0 {
			return nil
		}
		return logChange(ctx, tx, by, "import", "generators", fmt.Sprintf("%d geradores de NPC", n))
	})
	return n, err
}

// GeneratorFilter narrows the panel's list.
type GeneratorFilter struct {
	Q     string // id, or part of the leader's or follower's name
	Limit int
}

// SearchGenerators lists generators and says how many match.
func (s *Store) SearchGenerators(ctx context.Context, f GeneratorFilter) ([]Generator, int, error) {
	q := strings.TrimSpace(f.Q)
	id, err := strconv.Atoi(q)
	if err != nil {
		id = -1
	}
	where := `WHERE $1 = '' OR id = $2 OR lower(leader) LIKE $3 OR lower(follower) LIKE $3`
	args := []any{q, id, likePattern(q)}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM gamedata.npc_generators `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT `+generatorCols+` FROM gamedata.npc_generators `+where+` ORDER BY id LIMIT $4`, append(args, f.Limit)...)
	list, err := pgx.CollectRows(rows, scanGenerator)
	return list, total, err
}

func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
}

// GetGenerator loads one generator.
func (s *Store) GetGenerator(ctx context.Context, id int) (*Generator, error) {
	rows, _ := s.pool.Query(ctx, `SELECT `+generatorCols+` FROM gamedata.npc_generators WHERE id = $1`, id)
	g, err := pgx.CollectExactlyOneRow(rows, scanGenerator)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &g, err
}

// NextGeneratorID is the first id after the last generator.
func (s *Store) NextGeneratorID(ctx context.Context) (int, error) {
	var id int
	err := s.pool.QueryRow(ctx, `SELECT coalesce(max(id) + 1, 0) FROM gamedata.npc_generators`).Scan(&id)
	return id, err
}

// SaveGenerator adds or replaces one generator.
func (s *Store) SaveGenerator(ctx context.Context, g *Generator, by string) error {
	if err := g.Check(); err != nil {
		return err
	}
	row, err := generatorRow(g)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.npc_generators (`+generatorCols+`)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)
			ON CONFLICT (id) DO UPDATE SET minute = EXCLUDED.minute, max_mobs = EXCLUDED.max_mobs, min_group = EXCLUDED.min_group,
				max_group = EXCLUDED.max_group, formation = EXCLUDED.formation, route = EXCLUDED.route, leader = EXCLUDED.leader,
				follower = EXCLUDED.follower, segments = EXCLUDED.segments, fight_actions = EXCLUDED.fight_actions,
				die_actions = EXCLUDED.die_actions`, row...); err != nil {
			return err
		}
		return logChange(ctx, tx, by, "generator", strconv.Itoa(g.ID),
			fmt.Sprintf("%s / %s, a cada %d, máx. %d", g.Leader, g.Follower, g.Minute, g.MaxMobs))
	})
}

// DeleteGenerator empties one generator (its id is not reused by the others).
func (s *Store) DeleteGenerator(ctx context.Context, id int, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM gamedata.npc_generators WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return logChange(ctx, tx, by, "generator", strconv.Itoa(id), "removido")
	})
}

// GeneratorsUsing lists the ids of the generators that spawn a template.
func (s *Store) GeneratorsUsing(ctx context.Context, name string) ([]int, error) {
	rows, _ := s.pool.Query(ctx, `SELECT id FROM gamedata.npc_generators WHERE leader = $1 OR follower = $1 ORDER BY id`, name)
	return pgx.CollectRows(rows, pgx.RowTo[int])
}

// SearchMobTemplates lists templates of one kind by part of the name, and
// says how many match.
func (s *Store) SearchMobTemplates(ctx context.Context, kind, q string, limit int) ([]MobTemplate, int, error) {
	q = strings.TrimSpace(q)
	where := `WHERE kind = $1 AND ($2 = '' OR lower(name) LIKE $3)`
	args := []any{kind, q, likePattern(q)}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM gamedata.mob_templates `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT kind, name, content FROM gamedata.mob_templates `+where+` ORDER BY name LIMIT $4`, append(args, limit)...)
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MobTemplate, error) {
		var t MobTemplate
		return t, r.Scan(&t.Kind, &t.Name, &t.Content)
	})
	return list, total, err
}

// GetMobTemplate reads one template image.
func (s *Store) GetMobTemplate(ctx context.Context, kind, name string) ([]byte, error) {
	var content []byte
	err := s.pool.QueryRow(ctx, `SELECT content FROM gamedata.mob_templates WHERE kind = $1 AND name = $2`, kind, name).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return content, err
}

// SaveMobTemplate adds or replaces one template image. With create it
// refuses a name that already exists.
func (s *Store) SaveMobTemplate(ctx context.Context, t MobTemplate, create bool, by, summary string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := `INSERT INTO gamedata.mob_templates (kind, name, content) VALUES ($1, $2, $3)`
		if !create {
			q += ` ON CONFLICT (kind, name) DO UPDATE SET content = EXCLUDED.content`
		}
		if _, err := tx.Exec(ctx, q, t.Kind, t.Name, t.Content); err != nil {
			return err
		}
		return logChange(ctx, tx, by, "npc", t.Kind+"/"+t.Name, summary)
	})
}

// DeleteMobTemplate removes one template.
func (s *Store) DeleteMobTemplate(ctx context.Context, kind, name, by string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM gamedata.mob_templates WHERE kind = $1 AND name = $2`, kind, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return logChange(ctx, tx, by, "npc", kind+"/"+name, "removido")
	})
}
