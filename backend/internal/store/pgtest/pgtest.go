// Package pgtest gives integration tests an isolated, throw-away Postgres database.
//
// It reads TEST_DATABASE_URL (e.g. postgres://jarvis:jarvis@localhost:5432/jarvis_test?sslmode=disable,
// set by `make test-go`) and skips the test when it is unset. NewDatabase creates a fresh database
// named jarvis_t_<random> on that server and drops it when the test ends, so tests never touch
// shared data and can run alongside other packages' integration tests. If the role may not create
// databases, it falls back to resetting the public schema of TEST_DATABASE_URL itself.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvVar is the variable holding the integration test DSN.
const EnvVar = "TEST_DATABASE_URL"

// DSN returns TEST_DATABASE_URL or skips the test.
func DSN(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skip(EnvVar + " not set (run `make up` and export it, or use `make test-go`)")
	}
	return dsn
}

// NewDatabase returns a pool connected to a new empty database (no migrations applied).
func NewDatabase(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn := DSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "jarvis_t_" + hex.EncodeToString(b[:])

	if _, err := admin.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		admin.Close()
		t.Logf("pgtest: CREATE DATABASE failed (%v); resetting schema of %s instead", err, EnvVar)
		return resetSchema(t, dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		admin.Close()
		t.Fatalf("pgtest: parse dsn: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		admin.Close()
		t.Fatalf("pgtest: connect %s: %v", name, err)
	}
	t.Cleanup(func() {
		pool.Close()
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if _, err := admin.Exec(cctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
			t.Logf("pgtest: drop %s: %v", name, err)
		}
		admin.Close()
	})
	return pool
}

func resetSchema(t testing.TB, dsn string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgtest: connect: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		pool.Close()
		t.Fatalf("pgtest: reset schema: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
