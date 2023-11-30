package cmd

import (
	"github.com/spf13/cobra"
	"gitlab.bbdev.team/vh/pay/orders/api"
)

func init() {
	rootCmd.AddCommand(serverCmd)
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "orders service api",
	Run:   serverFn,
}

func serverFn(cmd *cobra.Command, args []string) {
	app := api.NewApp()
	app.Initialize()
	defer app.Shutdown()
	app.Run()
}
