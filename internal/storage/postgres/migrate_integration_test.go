//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"hookline/internal/storage/postgres"
	"hookline/internal/testdb"
)

func TestMigrateFreshExistingAndRepeatable(t *testing.T) {
	t.Parallel()
	db := testdb.OpenUnmigrated(t)
	ctx := context.Background()
	if err := postgres.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate fresh database: %v", err)
	}

	var tableCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('apps','endpoints','subscriptions','events','messages','attempts')`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 6 {
		t.Fatalf("created %d domain tables, want 6", tableCount)
	}

	const appID = "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec(ctx, `INSERT INTO apps(id,name,api_key_hash,github_webhook_secret) VALUES($1::uuid,'preserved','migration-preserved-key','github-webhook-secret')`, appID); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, db); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	assertPreservedApp(t, db, appID)

	if _, err := db.Exec(ctx, `DROP TABLE goose_db_version; CREATE TABLE hookline_schema_migrations(version bigint PRIMARY KEY); INSERT INTO hookline_schema_migrations(version) VALUES(1),(2),(3)`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	assertPreservedApp(t, db, appID)

	var applied int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM goose_db_version WHERE is_applied AND version_id IN (1,2,3)`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 3 {
		t.Fatalf("bridged %d legacy versions, want 3", applied)
	}
}

func assertPreservedApp(t *testing.T, db *pgxpool.Pool, id string) {
	t.Helper()
	var name string
	if err := db.QueryRow(context.Background(), `SELECT name FROM apps WHERE id=$1::uuid`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "preserved" {
		t.Fatalf("app name=%q, want preserved", name)
	}
}
