package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Names of gamedata.assets rows and kinds of gamedata.mob_templates rows.
const (
	AssetHeightMap    = "HeightMap.dat"
	AssetAttributeMap = "AttributeMap.dat"
	AssetItemList     = "ItemList.bin"

	TemplateNPC     = "npc"
	TemplateNPCBase = "npc_base"
	TemplateClass   = "class"
)

// WorldAsset is one binary file of the game world.
type WorldAsset struct {
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

// MobTemplate is one STRUCT_MOB image.
type MobTemplate struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

// WorldAssets is everything in gamedata.assets and gamedata.mob_templates.
type WorldAssets struct {
	Assets    []WorldAsset  `json:"assets"`
	Templates []MobTemplate `json:"templates"`
}

// Asset returns one file (nil if missing).
func (w *WorldAssets) Asset(name string) []byte {
	for i := range w.Assets {
		if w.Assets[i].Name == name {
			return w.Assets[i].Content
		}
	}
	return nil
}

// Require returns one file, which must have at least size bytes (and is cut
// to it).
func (w *WorldAssets) Require(name string, size int) ([]byte, error) {
	b := w.Asset(name)
	if b == nil {
		return nil, fmt.Errorf("the database has no %s: import a server export in the panel, or run: admin world-import", name)
	}
	if len(b) < size {
		return nil, fmt.Errorf("%s: %d bytes, want %d", name, len(b), size)
	}
	return b[:size], nil
}

// TemplatesOf returns the images of one kind by name.
func (w *WorldAssets) TemplatesOf(kind string) map[string][]byte {
	out := map[string][]byte{}
	for i := range w.Templates {
		if w.Templates[i].Kind == kind {
			out[w.Templates[i].Name] = w.Templates[i].Content
		}
	}
	return out
}

// LoadWorldAssets reads the whole world (TMSRV startup, server export).
func (s *Store) LoadWorldAssets(ctx context.Context) (*WorldAssets, error) {
	w := &WorldAssets{}
	rows, err := s.pool.Query(ctx, `SELECT name, content FROM gamedata.assets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a WorldAsset
		if err := rows.Scan(&a.Name, &a.Content); err != nil {
			rows.Close()
			return nil, err
		}
		w.Assets = append(w.Assets, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.pool.Query(ctx, `SELECT kind, name, content FROM gamedata.mob_templates ORDER BY kind, name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t MobTemplate
		if err := rows.Scan(&t.Kind, &t.Name, &t.Content); err != nil {
			rows.Close()
			return nil, err
		}
		w.Templates = append(w.Templates, t)
	}
	return w, rows.Err()
}

// ClassTemplates reads the images of the class templates (DBSRV startup).
func (s *Store) ClassTemplates(ctx context.Context) (map[string][]byte, error) {
	rows, err := s.pool.Query(ctx, `SELECT name, content FROM gamedata.mob_templates WHERE kind = $1`, TemplateClass)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var name string
		var content []byte
		if err := rows.Scan(&name, &content); err != nil {
			return nil, err
		}
		out[name] = content
	}
	return out, rows.Err()
}

// AssetUpdatedAt says when an asset last changed (ok false if missing).
func (s *Store) AssetUpdatedAt(ctx context.Context, name string) (at time.Time, ok bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT updated_at FROM gamedata.assets WHERE name = $1`, name).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return at, false, nil
	}
	return at, err == nil, err
}

// LoadAsset reads one asset (nil if missing).
func (s *Store) LoadAsset(ctx context.Context, name string) ([]byte, time.Time, error) {
	var content []byte
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT content, updated_at FROM gamedata.assets WHERE name = $1`, name).Scan(&content, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, at, nil
	}
	return content, at, err
}

// UpsertWorldAssets adds or replaces the given rows and leaves the others.
func (s *Store) UpsertWorldAssets(ctx context.Context, w *WorldAssets) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return upsertWorldAssets(ctx, tx, w)
	})
}

func upsertWorldAssets(ctx context.Context, tx pgx.Tx, w *WorldAssets) error {
	for _, a := range w.Assets {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.assets (name, content) VALUES ($1, $2)
			ON CONFLICT (name) DO UPDATE SET content = EXCLUDED.content, updated_at = now()
			WHERE gamedata.assets.content IS DISTINCT FROM EXCLUDED.content`, a.Name, a.Content); err != nil {
			return fmt.Errorf("asset %s: %w", a.Name, err)
		}
	}
	for _, t := range w.Templates {
		if _, err := tx.Exec(ctx, `INSERT INTO gamedata.mob_templates (kind, name, content) VALUES ($1, $2, $3)
			ON CONFLICT (kind, name) DO UPDATE SET content = EXCLUDED.content`, t.Kind, t.Name, t.Content); err != nil {
			return fmt.Errorf("template %s/%s: %w", t.Kind, t.Name, err)
		}
	}
	return nil
}

// replaceWorldAssets makes the tables equal to w (server import).
func replaceWorldAssets(ctx context.Context, tx pgx.Tx, w *WorldAssets) error {
	if _, err := tx.Exec(ctx, `DELETE FROM gamedata.mob_templates`); err != nil {
		return err
	}
	names := make([]string, 0, len(w.Assets))
	for _, a := range w.Assets {
		names = append(names, a.Name)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM gamedata.assets WHERE NOT (name = ANY($1))`, names); err != nil {
		return err
	}
	return upsertWorldAssets(ctx, tx, w)
}

// AccountLock reads an account's lock (nil if it has none).
func (s *Store) AccountLock(ctx context.Context, username string) ([]byte, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM account_locks WHERE username = $1`, strings.ToUpper(username)).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return data, err
}

// SetAccountLock stores an account's lock.
func (s *Store) SetAccountLock(ctx context.Context, username string, data []byte) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO account_locks (username, data) VALUES ($1, $2)
		ON CONFLICT (username) DO UPDATE SET data = EXCLUDED.data, updated_at = now()`, strings.ToUpper(username), data)
	return err
}

// DeleteAccountLock removes an account's lock.
func (s *Store) DeleteAccountLock(ctx context.Context, username string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM account_locks WHERE username = $1`, strings.ToUpper(username))
	return err
}
