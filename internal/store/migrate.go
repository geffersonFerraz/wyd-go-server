package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql migrations/world/*.sql
var migrationFS embed.FS

// migrationLockID is an arbitrary pg_advisory_lock key; dbsrv and the admin
// panel both migrate at startup and must not race.
const migrationLockID = 7514_0001

// Migrate brings the database up to date for this store's server, each file
// in its own transaction:
//
//   - migrations/NNNN_*.sql: what every server shares (recorded in
//     public.schema_migrations);
//   - migrations/world/NNNN_*.sql: the world of one server, applied to the
//     schema of this store's server, which is created if it is new (recorded
//     in its world_migrations). A change to a world table goes there, so it
//     reaches every server.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID) //nolint:errcheck

	// The shared migrations must not land in a server's schema.
	if _, err := conn.Exec(ctx, `SET search_path = public`); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), `RESET search_path`) //nolint:errcheck
	if err := migrateDir(ctx, conn.Conn(), "migrations", "public.schema_migrations"); err != nil {
		return err
	}

	schema := SchemaName(s.server)
	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+schema); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `SET search_path = `+schema+`, public`); err != nil {
		return err
	}
	if err := migrateDir(ctx, conn.Conn(), "migrations/world", schema+".world_migrations"); err != nil {
		return fmt.Errorf("server %d: %w", s.server, err)
	}
	return nil
}

// migrateDir applies the files of dir not yet recorded in table.
func migrateDir(ctx context.Context, conn *pgx.Conn, dir, table string) error {
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+table+` (
		version    text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}

	names, err := fs.Glob(migrationFS, dir+"/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, dir+"/"), ".sql")
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE version = $1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO `+table+` (version) VALUES ($1)`, version)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", version, err)
		}
	}
	return nil
}
