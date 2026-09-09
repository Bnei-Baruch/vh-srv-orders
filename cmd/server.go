package cmd

import (
	"context"
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
		utils.FatalAfter(app.Shutdown, "app.Initialize", slog.Any("err", err))
	}

	app.Run(ctx)

	// Signals go back to their default disposition before the drain, not after
	// it. signal.NotifyContext stops relaying once it has delivered one, so
	// leaving this to the defer meant a second Ctrl-C during the drain was
	// swallowed and the operator had no way to cut it short.
	stop()
}
