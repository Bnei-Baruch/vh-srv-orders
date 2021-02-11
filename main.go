package main

import (
	"log"

	"github.com/joho/godotenv"
)

//Conf store all conf
var Conf map[string]string

func main() {
	//Env file
	initConf()

	//Database
	initDB(Conf["DB"])

	//Setup router and run on PORT
	r := initRouter()
	log.Println("Service is up on port", Conf["PORT"])
	r.Run(Conf["PORT"])

	//Close DB on quit
	defer DB.Close()
}

func initConf() {
	conf, err := godotenv.Read() // Read env file without messing with actual ENV
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	Conf = conf
	// also loading in the env - TODO: cleanup that mess later
	err = godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}
