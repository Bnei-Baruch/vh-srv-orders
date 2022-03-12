package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/kelseyhightower/envconfig"
)

//DB is globally available
var DB *pgxpool.Pool

// cfg is the struct type that contains fields that stores the necessary configuration
// gathered from the environment.
var cfg struct {
	PgHost   string `envconfig:"DB_HOST" default:"localhost"`
	PgPort   string `envconfig:"DB_PORT" default:"5432"`
	PgUser   string `envconfig:"DB_USER" default:"postgres"`
	PgPass   string `envconfig:"DB_PASSWORD" default:"pass"`
	PgDbName string `envconfig:"DB_DATABASE" default:"gorm"`
}

//Init DB
func initDB(dbtype string) {
	connectPostgreSQL()
}

func connectPostgreSQL() {

	if err := envconfig.Process("LIST", &cfg); err != nil {
		log.Fatalln("Error while fetching env file")
		return
	}

	connec := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s",
		cfg.PgHost,
		cfg.PgPort,
		cfg.PgUser,
		cfg.PgDbName,
		cfg.PgPass,
	)

	fmt.Println("--cfg.PgHost: ", cfg.PgHost)
	fmt.Println("--cfg.PgPort: ", cfg.PgPort)
	fmt.Println("--cfg.PgUser: ", cfg.PgUser)
	fmt.Println("--cfg.PgPass: ", cfg.PgPass)
	fmt.Println("--cfg.PgDbName: ", cfg.PgDbName)

	fmt.Println("--connection-string--", connec)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.Connect(ctx, connec)
	if err != nil {
		fmt.Println("unable to connect to database: %w", err)
	}

	DB = pool
}
