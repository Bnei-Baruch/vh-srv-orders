package main

import (
	_ "github.com/joho/godotenv/autoload"

	"github.com/Bnei-Baruch/vh-srv-orders/cmd"
)

func main() {
	cmd.Execute()
}
