package charge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/volatiletech/null/v9"

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
	repo            repo.OrdersRepository
	eventEmitter    events.EventEmitter
	muhlafimFetcher MuhlafimFetcher
}

func NewCharger() *Charger {
	c := new(Charger)
	c.muhlafimFetcher = NewPelecardMuhlafimFetcher()
	return c
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

func (c *Charger) SetMuhlafimFetcher(f MuhlafimFetcher) {
	c.muhlafimFetcher = f
}

func (c *Charger) Charge() error {
	// locking ?! we need to ensure only one charge process

	ctx := context.Background()
	charge := new(repo.MonthlyCharge)
	charge.Props = make(map[string]interface{})
	charge.Props["timing"] = make(map[string]time.Duration)

	previousCharges, err := c.repo.GetMonthlyCharges(ctx, 0, 1)
	if err != nil {
		return fmt.Errorf("repo.GetMonthlyCharges: %w", err)
	}

	var lastCharge *repo.MonthlyCharge
	if len(previousCharges) > 0 {
		lastCharge = previousCharges[0]
		if lastCharge.Status == common.MonthlyChargeStatusInProgress {
			return errors.New("Another monthly charge is already in progress. Please abort it first")
		}

		// TODO (edo): consider different status of multiple previous charges.
		// at the moment I'm naive to consider the previous to have been completed successfuly.
		// Basically we need dates but do we continue where unsuccessful previous attempt left of?

		charge.StartDate = lastCharge.EndDate
		charge.EndDate = null.TimeFrom(time.Now().UTC())
	}
	charge.Status = common.MonthlyChargeStatusStarted
	charge.Month = int(time.Now().Month())
	charge.Year = time.Now().Year()

	charge.ID, err = c.repo.CreateMonthlyCharge(ctx, charge)
	if err != nil {
		return fmt.Errorf("repo.CreateMonthlyCharge: %w", err)
	}
	slog.Info("created new charge", slog.Int("id", charge.ID), slog.Int("month", charge.Month), slog.Int("year", charge.Year))

	// get orders to charge
	tStart := time.Now()
	ordersToCharge, err := c.repo.GetOrdersToCharge(ctx, charge.Year, charge.Month)
	if err != nil {
		return fmt.Errorf("GetOrdersToCharge: %w", err)
	}
	timing := time.Now().Sub(tStart)
	slog.Info("got orders to charge", slog.Int("count", len(ordersToCharge)), slog.Duration("timing", timing))
	charge.Props["orders_count"] = len(ordersToCharge)
	charge.Props["timing"].(map[string]time.Duration)["orders_count"] = timing

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
