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

func (im *Charger) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentCharger
	return event
}

func (c *Charger) Charge() error {
	// locking ?! we need to ensure only one charge process

	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, c)
	charge := new(MonthlyChargeContext)
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

	charge.ID, err = c.repo.CreateMonthlyCharge(ctx, &charge.MonthlyCharge)
	if err != nil {
		return fmt.Errorf("repo.CreateMonthlyCharge: %w", err)
	}
	slog.Info("created new charge", slog.Int("id", charge.ID), slog.Int("month", charge.Month), slog.Int("year", charge.Year))

	// get orders to charge
	tStart := time.Now()
	slog.Info("fetching orders to charge")
	ordersToCharge, err := c.repo.GetOrdersToCharge(ctx, charge.Year, charge.Month)
	if err != nil {
		return fmt.Errorf("GetOrdersToCharge: %w", err)
	}
	timing := time.Now().Sub(tStart)
	charge.Props["timing"].(map[string]time.Duration)["orders_count"] = timing
	slog.Info("got orders to charge", slog.Int("count", len(ordersToCharge)), slog.Duration("timing", timing))
	charge.Props["orders_count"] = len(ordersToCharge)
	// TODO (edo): save props to DB here

	// fetch muhlafim
	tStart = time.Now()
	slog.Info("fetching muhlafim")
	muhlafim, err := c.muhlafimFetcher.FetchMuhlafim(ctx, charge.StartDate.Time, charge.EndDate.Time)
	if err != nil {
		slog.Warn("muhlafim error", slog.Any("err", err))
	}
	timing = time.Now().Sub(tStart)
	charge.Props["timing"].(map[string]time.Duration)["fetch_muhlafim"] = timing
	slog.Info("got muhlafim", slog.Int("count", len(muhlafim)), slog.Duration("timing", timing))
	// TODO (edo): save props to DB here

	muhlafimToCancel := make(map[string]*TokenRedirect)
	for _, x := range muhlafim {
		switch x.ActionDescription {
		case muhlafimActionHiyuvNiklat:
			slog.Info("muhlafim hiyuv niklat", slog.String("token", x.Token))
		case muhlafimActionNidha:
			slog.Info("muhlafim nidha", slog.String("token", x.Token))
			muhlafimToCancel[x.Token] = x
		case muhlafimActionBitul:
			slog.Info("muhlafim bitul", slog.String("token", x.Token))
			muhlafimToCancel[x.Token] = x
		case muhlafimActionLoTakin:
			slog.Info("muhlafim lo takin", slog.String("token", x.Token))
			muhlafimToCancel[x.Token] = x
		default:
			slog.Info("muhlafim unknown action", slog.String("token", x.Token), slog.String("action", x.ActionDescription))
		}
	}
	slog.Info("muhlafim to cancel", slog.Int("count", len(muhlafimToCancel)))

	// cancel by cards
	tStart = time.Now()
	slog.Info("cancelling by cards")
	tokens := make([]string, 0)
	for k, _ := range muhlafimToCancel {
		tokens = append(tokens, k)
	}
	cards, err := c.repo.GetActiveCardsByTokens(ctx, tokens)
	if err != nil {
		slog.Warn("repo.GetActiveCardsByTokens error", slog.Any("err", err))
	} else {
		slog.Info("got active cards by tokens", slog.Int("count", len(cards)))
		for _, card := range cards {
			slog.Info("deactivating card", slog.Int("id", int(card.ID)))
			if orderIDs, err := c.repo.DeactivateCard(ctx, int(card.ID)); err != nil {
				slog.Warn("repo.DeactivateCard error", slog.Any("err", err), slog.Int("cardID", int(card.ID)))
			} else {
				slog.Info("deactivated card orders", slog.Int("count", len(orderIDs)), slog.Any("orderIDs", orderIDs))
				for _, orderID := range orderIDs {
					charge.ordersToCancel[orderID] = &OrderToCancel{OrderID: orderID, Redirect: muhlafimToCancel[card.Token.String]}
				}
				delete(muhlafimToCancel, card.Token.String)
			}
		}
	}
	timing = time.Now().Sub(tStart)
	charge.Props["timing"].(map[string]time.Duration)["cancel_by_cards"] = timing
	// TODO (edo): save props to DB here

	// cancel by payments
	tStart = time.Now()
	slog.Info("cancelling by payments")
	timing = time.Now().Sub(tStart)
	charge.Props["timing"].(map[string]time.Duration)["cancel_by_payments"] = timing
	// TODO (edo): save props to DB here

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
