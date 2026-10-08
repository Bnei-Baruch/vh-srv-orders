package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peterldowns/pgtestdb"
	"github.com/peterldowns/pgtestdb/migrators/golangmigrator"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// migrationsDir resolves the db/migrations directory relative to the project root.
// Works regardless of which package the test runs from.
func migrationsDir() string {
	// Try the GO_MIGRATE_DIR env var first (CI/custom setups).
	if dir := os.Getenv("GO_MIGRATE_DIR"); dir != "" {
		return dir
	}
	// Walk up from this source file to find the project root.
	_, thisFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(projectRoot, "db", "migrations")
}

// NewTestOrdersDB is a helper that returns an open connection to a unique and isolated
// test database, fully migrated and ready for testing, it will be deleted if the
// tests succeed and will NOT be deleted if tests fail.
func NewTestOrdersDB(t *testing.T, ctx context.Context) (string, error) {
	config := pgtestdb.Config{
		DriverName: "postgres",
		User:       common.Config.PgUser,
		Password:   common.Config.PgPass,
		Host:       common.Config.PgHost,
		Port:       common.Config.PgPort,
		Database:   url.QueryEscape(common.Config.PgDbName),
		// Timezone pinned so calendar arithmetic does not depend on the
		// developer's Postgres. As an `options` parameter because psql rejects
		// plain `timezone=`; quote the URL when pasting it there.
		Options: "sslmode=disable&options=-c%20timezone%3DUTC",
	}

	gm := golangmigrator.New(migrationsDir())
	if err := gm.Migrate(ctx, nil, config); err != nil {
		if err == migrate.ErrNoChange {
			fmt.Printf("Migrations ok, no change.\n")
		} else {
			return "", fmt.Errorf("gm.Migrate: %w", err)
		}
	}

	// Once: each call creates another instance database, and Custom logs the
	// URL itself.
	return pgtestdb.Custom(t, config, gm).URL(), nil
}

// NewTestPool opens a pool against an already-created test database, for tests
// that need to assert or seed database state directly.
//
// This exists because *repo.OrdersDB no longer exposes the pool: it holds it in
// a field rather than embedding it, so Exec and friends are not promoted onto
// the concrete type and a handler cannot reach the database past the repo layer
// — which is where the events live. Tests still legitimately need raw access,
// and they take it here rather than through an exported accessor that would put
// the same escape hatch back on production code.
func NewTestPool(t *testing.T, dbURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("testutil.NewTestPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
