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
		// Timezone pinned so results do not depend on the developer's Postgres:
		// SQL that resolves a calendar unit does so in the session timezone. An
		// An `options` startup parameter rather than `timezone=UTC`: both Go
		// drivers forward the plain spelling, but psql rejects it outright with
		// `invalid URI query parameter: "timezone"`, and the URL pgtestdb logs
		// exists to be pasted into psql. Quote it when doing so — unquoted, the
		// shell splits at the ampersand either way and the timezone is silently
		// dropped.
		//
		// The pin also hides the session-timezone dependence it compensates for,
		// so no test in CI can observe it: issue #20.
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

	// Called once. Calling Custom twice created two instance databases per test
	// and returned the second while logging the first, so pasting the logged URL
	// into psql inspected a database nothing had touched. Custom logs
	// "testdbconf: <url>" itself, so there is no t.Log here either.
	return pgtestdb.Custom(t, config, gm).URL(), nil
}
