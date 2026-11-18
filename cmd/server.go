package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/Bnei-Baruch/vh-srv-orders/api"
	"github.com/Bnei-Baruch/vh-srv-orders/domain/pricing"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
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
	if err := pricing.ValidateConfig(); err != nil {
		utils.LogFatal("pricing.ValidateConfig", slog.Any("err", err))
	}

	app := api.NewApp()
	app.Initialize()
	defer app.Shutdown()
	app.Run()
}
