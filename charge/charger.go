package charge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func MonthlyCharge() {
	slog.Info("running monthly charge")

	// Setup sentry
	sentryTransport := sentry.NewHTTPSyncTransport()
	sentryTransport.Timeout = 3 * time.Second
	err := sentry.Init(sentry.ClientOptions{
		Release:     common.GitSHA,
		Environment: common.Config.Env,
		Transport:   sentryTransport,
		Tags: map[string]string{
			"command": "charge",
		},
	})
	if err != nil {
		utils.LogFatal("sentry.Init", slog.Any("err", err))
	}
	defer sentry.Flush(2 * time.Second)

	// actual charging
	charger := NewCharger()
	if err := charger.Init(); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("charger.Init", slog.Any("err", err))
	}
	defer charger.Close()

	if err := charger.Charge(); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("charger.Charge", slog.Any("err", err))
	}

	slog.Info("monthly charge completed")
}

type Charger struct {
	repo         repo.OrdersRepository
	eventEmitter events.EventEmitter
}

func NewCharger() *Charger {
	return new(Charger)
}

func (c *Charger) Init() error {
	var err error

	c.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		return fmt.Errorf("events.CreateEmitter: %w", err)
	}

	c.repo, err = repo.NewOrdersDB(context.Background(), c.eventEmitter)
	if err != nil {
		return fmt.Errorf("repo.NewOrdersDB: %w", err)
	}

	return nil
}

func (c *Charger) Close() {
	c.repo.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	c.eventEmitter.Close(ctx)
}

func (c *Charger) Charge() error {
	// period start and end dates
	// cleanup userkey
	// clear flags
	// flag orders for renewal
	// skip orders (last month + this month)
	// pelecard muhlafim
	// charge masof horaat keva
	// charge masof ragil
	// generate report
	// send report and logs

	return nil
}
