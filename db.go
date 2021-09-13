package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

//DB is globally available
var DB *gorm.DB

//PgHost default value
const PgHost = "localhost"

//PgPort default value
const PgPort = "5432"

//PgUser default value
const PgUser = "user"

//PgPass default value
const PgPass = "pass"

//PgSSL default value
const PgSSL = "disable"

//PgDbName default value
const PgDbName = "PGDATABASE"

//Init DB
func initDB(dbtype string) {
	switch dbtype {
	case "sqlite":
		connectSqlite()
	case "pg":
		connectPostgreSQL()
	case "mockdb":
		connectMockdb()
	default:
		log.Fatal("Unknown or undefined DB")
	}

	DB.AutoMigrate(&Order{})
	DB.AutoMigrate(&Payment{})
	DB.AutoMigrate(&Invoice{})
	DB.AutoMigrate(&Account{})
}

func connectSqlite() {
	db, err := gorm.Open("sqlite3", Conf["DB_FILE"])
	if err != nil {
		log.Fatal("Failed to connect to database with error", err)
	}

	DB = db
}

func connectMockdb() {
	db, err := gorm.Open("sqlite3", "test.db")
	if err != nil {
		log.Fatal("Failed to connect to database with error", err)
	}

	DB = db
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

func connectPostgreSQL() {
	connec := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=%s",
		getEnv("PGHOST", PgHost),
		getEnv("PGPORT", PgPort),
		getEnv("PGUSER", PgUser),
		getEnv("PGDATABASE", PgDbName),
		getEnv("PGPASSWORD", PgPass),
		getEnv("PG_SSLMODE", PgSSL),
	)

	db, err := gorm.Open("postgres", connec)

	if err != nil {
		log.Fatal("Failed to connect to database with error \n", err, "\n", connec)
	}

	DB = db
}
