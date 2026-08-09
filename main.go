package main

import (
	_ "time/tzdata"

	_ "github.com/joho/godotenv/autoload"

	"gitlab.bbdev.team/vh/pay/orders/cmd"
)

func main() {
	cmd.Execute()
}
