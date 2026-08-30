package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/migrations"
)

func Migrate(c context.Context, p *pgxpool.Pool) error {
	t, e := p.Begin(c)
	if e != nil {
		return e
	}
	defer func() { _ = t.Rollback(c) }()
	if _, e = t.Exec(c, `SELECT pg_advisory_xact_lock(4815162342)`); e != nil {
		return e
	}
	if _, e = t.Exec(c, `CREATE TABLE IF NOT EXISTS hookline_schema_migrations(version bigint PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
		return e
	}
	entries, e := migrations.FS.ReadDir(".")
	if e != nil {
		return e
	}
	for _, x := range entries {
		if x.IsDir() || !strings.HasSuffix(x.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(x.Name(), "_")
		if !ok {
			return fmt.Errorf("invalid migration %s", x.Name())
		}
		v, e := strconv.ParseInt(prefix, 10, 64)
		if e != nil {
			return e
		}
		var done bool
		if e = t.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM hookline_schema_migrations WHERE version=$1)`, v).Scan(&done); e != nil {
			return e
		}
		if done {
			continue
		}
		b, e := migrations.FS.ReadFile(x.Name())
		if e != nil {
			return e
		}
		up, _, ok := strings.Cut(string(b), "-- +goose Down")
		if !ok {
			return fmt.Errorf("missing Down marker: %s", x.Name())
		}
		if _, e = t.Exec(c, up); e != nil {
			return fmt.Errorf("migration %d: %w", v, e)
		}
		if _, e = t.Exec(c, `INSERT INTO hookline_schema_migrations(version) VALUES($1)`, v); e != nil {
			return e
		}
	}
	return t.Commit(c)
}
