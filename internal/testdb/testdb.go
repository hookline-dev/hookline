//go:build integration

package testdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"hookline/internal/domain"
	storepostgres "hookline/internal/storage/postgres"
)

func Open(t testing.TB) *pgxpool.Pool {
	return open(t, true)
}

// OpenUnmigrated creates an isolated PostgreSQL schema without applying
// Hookline migrations. It is intended for migration compatibility tests.
func OpenUnmigrated(t testing.TB) *pgxpool.Pool {
	return open(t, false)
}

func open(t testing.TB, migrate bool) *pgxpool.Pool {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		box, e := tc.Run(c, "postgres:16-alpine", tc.WithDatabase("hookline"), tc.WithUsername("hookline"), tc.WithPassword("hookline"), tc.BasicWaitStrategies())
		if e != nil {
			t.Fatal(e)
		}
		testcontainers.CleanupContainer(t, box)
		u, e = box.ConnectionString(c, "sslmode=disable")
		if e != nil {
			t.Fatal(e)
		}
	}
	admin, e := pgxpool.New(c, u)
	if e != nil {
		t.Fatal(e)
	}
	id, _ := domain.NewID()
	schema := "test_" + strings.ReplaceAll(id, "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(c, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		x, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = admin.Exec(x, "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
	})
	cfg, e := pgxpool.ParseConfig(u)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(c, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	if migrate {
		e = storepostgres.Migrate(c, p)
	}
	if e != nil {
		t.Fatal(e)
	}
	return p
}
