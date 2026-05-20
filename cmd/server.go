package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"

	"gitlab.bbdev.team/vh/pay/orders/api"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
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
