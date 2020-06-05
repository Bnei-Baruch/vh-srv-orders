package main

import (
	"fmt"
	"log"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

//DB is ...
var DB *gorm.DB

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

func connectPostgreSQL() {
	connec := fmt.Sprintf("host=%s port=%s user=%s dbname=%s password=%s sslmode=%s",
		Conf["PG_HOST"],
		Conf["PG_PORT"],
		Conf["PG_USER"],
		Conf["PG_DBNAME"],
		Conf["PG_PWD"],
		Conf["PG_SSLMODE"])

	db, err := gorm.Open("postgres", connec)

	if err != nil {
		log.Fatal("Failed to connect to database with error", err)
	}

	DB = db
}
