package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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

	// Registered before anything is built, so a signal during Initialize —
	// migrations, the JWKS fetch — is caught rather than killing the process
	// with NATS already up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := api.NewApp()
	// Deferred before Initialize, so a failure part-way through startup still
	// drains what was built.
	defer app.Shutdown()

	if err := app.Initialize(ctx); err != nil {
		// Signals go back to their default disposition before any drain, so a
		// second Ctrl-C can cut a slow one short. signal.NotifyContext stops
		// relaying once it has delivered one, so leaving this to the deferred
		// stop meant every later signal was swallowed.
		stop()

		// A signal during startup is a stop, not a failure. Reported as a
		// failure it would be `docker compose up -d` recreating the container
		// mid-migration and the old process exiting 1 — a clean shutdown
		// recorded as a crashed startup by Docker and anything reading exit
		// codes.
		if errors.Is(err, context.Canceled) {
			slog.Info("signal received during startup, shutting down")
			return
		}

		utils.FatalAfter(app.Shutdown, "app.Initialize", slog.Any("err", err))
	}

	app.Run(ctx, stop)
	stop()
}
