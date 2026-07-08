package cmd

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

type worker interface {
	Init() error
	Close()
	DoTask() error
	String() string
}

func (w *Worker) String() string {
	return "SpecialActivator Worker"
}

type Worker struct {
	repo         repo.OrdersRepository
	eventEmitter events.EventEmitter
	eventBuilder events.EventBuilder
}

func NewWorker() *Worker {
	return new(Worker)
}

func Do(w *Worker) {
	slog.Info("running worker", slog.String("worker", w.String()))

	// Setup sentry
	sentryTransport := sentry.NewHTTPSyncTransport()
	sentryTransport.Timeout = 3 * time.Second
	err := sentry.Init(sentry.ClientOptions{
		Release:     common.GitSHA,
		Environment: common.Config.Env,
		Transport:   sentryTransport,
		Tags: map[string]string{
			"command": "worker " + w.String(),
		},
	})
	if err != nil {
		utils.LogFatal("sentry.Init", slog.Any("err", err))
	}
	defer sentry.Flush(2 * time.Second)

	// do the thing
	if err := w.Init(); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("worker.Init", slog.Any("err", err))
	}

	if err := w.DoTask(); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("im.DoTask", slog.Any("err", err))
	}

	w.Close()

	slog.Info("worker task completed", slog.String("worker", w.String()))
}

func (w *Worker) Init() error {
	var err error

	w.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		return fmt.Errorf("events.CreateEmitter: %w", err)
	}

	w.repo, err = repo.NewOrdersDB(context.Background(), w.eventEmitter)
	if err != nil {
		return fmt.Errorf("repo.NewOrdersDB: %w", err)
	}

	return nil
}

func (w *Worker) Close() {
	w.repo.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	w.eventEmitter.Close(ctx)
}

func (w *Worker) DoTask() error {

	emails, err := w.repo.GetUniqueEmailsFromSpecial(context.Background())
	if err != nil {
		return fmt.Errorf("repo.GetUniqueEmailsFromSpecial: %w", err)
	}
	for _, email := range emails {

		specials, err := w.repo.GetAllSpecialsByEmail(context.Background(), email)
		if err != nil {
			return fmt.Errorf("repo.GetAllSpecials: %w", err)
		}
		var actualSpecial *repo.Special
		for _, special := range specials {
			if isBeginsToday(special) {
				if actualSpecial == nil {
					actualSpecial = special
					continue
				}
				if special.EndDate.Time.After(actualSpecial.EndDate.Time) {
					actualSpecial = special
				}
			}
		}
		if actualSpecial != nil {
			ctx := context.WithValue(context.Background(), common.CtxEventBuilder, w)
			w.emitEvent(ctx,
				events.TypeCreateSpecial,
				map[string]interface{}{
					"email":       actualSpecial.Email,
					"keycloak_id": actualSpecial.KeycloakId,
					"start_date":  actualSpecial.StartDate,
					"end_date":    actualSpecial.EndDate})
		}

	}
	return nil
}

func isBeginsToday(special *repo.Special) bool {
	if !special.StartDate.Valid {
		return false
	}
	startYear, startMonth, startDay := special.StartDate.Time.Local().Date()
	nowYear, nowMonth, nowDay := time.Now().Date()
	return startYear == nowYear && startMonth == nowMonth && startDay == nowDay
}

func (w *Worker) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentSpecialActivator
	event.Actor = events.ActorSystem
	return event
}

func (w *Worker) emitEvent(ctx context.Context, eventType string, payload map[string]interface{}) {
	builder := ctx.Value(common.CtxEventBuilder).(events.EventBuilder)
	event := builder.BuildEvent(eventType, payload)
	w.eventEmitter.Emit(ctx, event)
}
