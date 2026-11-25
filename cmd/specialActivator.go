package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.eventEmitter.Close(ctx)
}

// DoTask emits one activation per person whose special begins today. It reads
// specials by start date rather than by distinct email: a special may have no
// email, and an email-keyed loop missed or merged those.
func (w *Worker) DoTask() error {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// A day either side; isBeginsToday decides on the local date.
	specials, err := w.repo.GetSpecialsStartingBetween(context.Background(),
		dayStart.AddDate(0, 0, -1), dayStart.AddDate(0, 0, 2))
	if err != nil {
		return fmt.Errorf("repo.GetSpecialsStartingBetween: %w", err)
	}

	beginningToday := make([]*repo.Special, 0, len(specials))
	for _, special := range specials {
		if !isBeginsToday(special) {
			continue
		}
		// Revoking moves end_date into the past; without this the next tick
		// re-activates a special revoked today.
		if !special.EndDate.Time.After(now) {
			slog.Info("special already ended, not activating",
				slog.Int("special_id", special.Id.Int), slog.Time("end_date", special.EndDate.Time))
			continue
		}
		if len(identifiersOf(special)) == 0 {
			slog.Warn("special with no identifier cannot be activated", slog.Int("special_id", special.Id.Int))
			continue
		}
		beginningToday = append(beginningToday, special)
	}

	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, w)
	activated := 0
	for _, group := range groupByPerson(beginningToday) {
		longest := group[0]
		for _, special := range group[1:] {
			if special.EndDate.Time.After(longest.EndDate.Time) {
				longest = special
			}
		}
		w.emitEvent(ctx,
			events.TypeCreateSpecial,
			// vh-srv-profile reads both identifiers with unchecked string
			// assertions, so an absent one goes out as "", never null.
			map[string]interface{}{
				"email":       longest.Email.String,
				"keycloak_id": longest.KeycloakId.String,
				"start_date":  longest.StartDate,
				"end_date":    longest.EndDate})
		activated++
	}
	slog.Info("activation summary", slog.Int("activated", activated), slog.Int("considered", len(specials)))
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
