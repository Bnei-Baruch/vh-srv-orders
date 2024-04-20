package repo

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

func GetDBURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		url.QueryEscape(common.Config.PgUser),
		url.QueryEscape(common.Config.PgPass),
		common.Config.PgHost,
		common.Config.PgPort,
		url.QueryEscape(common.Config.PgDbName))
}

func SyncDBStructInsertionAndMigrations() error {
	slog.Info("running db migrations")
	m, err := migrate.New(
		"file://./db/migrations", GetDBURL()+"?sslmode=disable")
	if err != nil {
		return fmt.Errorf("migrate.New: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			slog.Info("no changes in migrations")
			return nil
		}
		return fmt.Errorf("migrate.Up: %w", err)
	}

	return nil
}
