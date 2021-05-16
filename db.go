package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

//DB is ...
var DB *gorm.DB
const DEFAULT_PG_HOST = "localhost"
const DEFAULT_PG_PORT = "5432"
const DEFAULT_PG_USER = "user"
const DEFAULT_PG_PASS = "pass"
const DEFAULT_PG_SSL = "disable"
const DEFAULT_PG_DBNAME = "PGDATABASE"

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
		getEnv("PGHOST" , DEFAULT_PG_HOST),
		getEnv("PGPORT", DEFAULT_PG_PORT),
		getEnv("PGUSER", DEFAULT_PG_USER),
		getEnv("PGDATABASE", DEFAULT_PG_DBNAME ),
		getEnv("PGPASSWORD", DEFAULT_PG_PASS),
		getEnv("PG_SSLMODE", DEFAULT_PG_SSL),
	)
	//Conf["PG_HOST"],
	//Conf["PG_PORT"],
	//Conf["PG_USER"],
	//Conf["PG_DBNAME"],
	//Conf["PG_PWD"],
	//Conf["PG_SSLMODE"])

	db, err := gorm.Open("postgres", connec)

	if err != nil {
		log.Fatal(connec)
		log.Fatal("Failed to connect to database with error", err)
	}

	DB = db
}
