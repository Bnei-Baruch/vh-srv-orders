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
		// The session timezone is pinned rather than inherited: Postgres does
		// interval arithmetic in the session TimeZone, so
		// `ts + make_interval(months => n)` lands on a different instant depending
		// on it. The CI image defaults to Etc/UTC; a native or Homebrew server set
		// to Asia/Jerusalem would shift results in tests that look timezone-free.
		//
		// Spelled as an `options` startup parameter, not `timezone=UTC`. The
		// latter is not a libpq keyword and survives only because lib/pq and pgx
		// are pure Go and forward unknown parameters — psql rejects the URL this
		// function logs with `invalid URI query parameter: "timezone"`, which
		// breaks the copy-paste-into-psql path the log line exists for.
		//
		// Test sessions only. Production connections (repo.GetDBURL) pin nothing,
		// so a server in a non-UTC timezone computes grant end dates in that
		// timezone.
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

	// One instance, logged and returned. Calling Custom twice created two
	// instance databases per test and logged the URL of the one nobody connected
	// to, so pasting it into psql inspected an empty database.
	instance := pgtestdb.Custom(t, config, gm)
	t.Log("testdbconf:", instance.URL())
	return instance.URL(), nil
}
