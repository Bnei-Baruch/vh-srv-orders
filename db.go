package main

import (
	"context"
	"fmt"
	"orderservices/orders/utils"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

// DB is globally available
var DB *pgxpool.Pool

// cfg is the struct type that contains fields that stores the necessary configuration
// gathered from the environment.
// Init DB
func initDB(dbtype string) {
	connectPostgreSQL()
}

func connectPostgreSQL() {

	connec := utils.GetDBURL()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.Connect(ctx, connec)
	if err != nil {
		fmt.Println("unable to connect to database: %w", err)
	}

	DB = pool
}
