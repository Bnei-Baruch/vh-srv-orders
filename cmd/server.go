package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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

	// Before anything is built, so a signal during Initialize is caught too.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := api.NewApp()
	// Before Initialize, so a failed startup still drains what was built.
	defer app.Shutdown()

	if err := app.Initialize(ctx, stop); err != nil {
		// Restore default signal handling before draining, so a second Ctrl-C
		// can cut a slow drain short.
		stop()

		// A signal during startup is a clean stop (exit 0), not a crash.
		if errors.Is(err, context.Canceled) {
			slog.Info("signal received during startup, shutting down")
			return
		}

		utils.FatalAfter(app.Shutdown, "app.Initialize", slog.Any("err", err))
	}

	app.Run(ctx)
	stop()
}
