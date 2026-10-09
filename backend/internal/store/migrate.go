package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/migrations"
)

// migrateLockID is the pg_advisory_lock key used to serialise concurrent migrators.
const migrateLockID = 7_420_001

// Migrate applies all pending embedded migrations inside one transaction per file, recording each
// in schema_migrations. It is safe to call concurrently (advisory lock) and repeatedly (idempotent).
// It returns the names of newly applied migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	return migrateFS(ctx, pool, migrations.FS)
}

func migrateFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return nil, fmt.Errorf("migrate: lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrateLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	names, err := fs.Glob(fsys, "*.up.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	applied := map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: list applied: %w", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		applied[n] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var done []string
	for _, name := range names {
		key := strings.TrimSuffix(name, ".up.sql")
		if applied[key] {
			continue
		}
		sqlBytes, err := fs.ReadFile(fsys, name)
		if err != nil {
			return done, err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, key)
			return err
		})
		if err != nil {
			return done, fmt.Errorf("migrate: apply %s: %w", name, err)
		}
		done = append(done, key)
	}
	return done, nil
}
