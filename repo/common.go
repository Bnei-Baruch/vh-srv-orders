package repo

import (
	"fmt"
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
	fmt.Println("Starting DB Migration")
	m, err := migrate.New(
		"file://./db/migrations", GetDBURL()+"?sslmode=disable")
	if err != nil {
		fmt.Println("Error while creating migration instance ::", err)
		return err
	}
	// Syncing Table struct (UP Mig), Insertion ( Up Mig ) & UP Migrations
	if err := m.Up(); err != nil {
		m.Close()
		if err == migrate.ErrNoChange {
			fmt.Println("No changes in UP migration")
			return nil
		}
		return err
	}
	m.Close()
	fmt.Println("UP Migration Done!")
	return nil
}
