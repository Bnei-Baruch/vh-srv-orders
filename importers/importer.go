package importers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/keycloak"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/profiles"
	"github.com/Bnei-Baruch/vh-srv-orders/pkg/utils"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

type importer interface {
	String() string
	Init() error
	Close()
	Import() error
}

// reportDroppedRows sends dropped sheet rows to Sentry: a Warning at any
// ratio, an Error when nothing survived. A dropped row is not fatal, so
// without this a changed date format would import nothing and exit 0.
//
// The fingerprint buckets the drop count: sheets are never cleared, so the
// same unfixable row is re-dropped on every run, and exact counts would open a
// new issue each time the sheet grows.
func reportDroppedRows(im importer, dropped, kept int) {
	if dropped == 0 {
		return
	}
	slog.Warn("importer dropped rows", slog.String("importer", im.String()), slog.Int("dropped", dropped), slog.Int("kept", kept))

	level := sentry.LevelWarning
	message := fmt.Sprintf("%s: dropped %d of %d sheet rows as malformed", im.String(), dropped, dropped+kept)
	if kept == 0 {
		level = sentry.LevelError
		message = fmt.Sprintf("%s: dropped every one of %d sheet rows as malformed; the sheet format may have changed", im.String(), dropped)
	}
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)
		scope.SetFingerprint([]string{"importer-dropped-rows", im.String(), dropBucket(dropped, kept)})
		sentry.CaptureMessage(message)
	})
}

// dropBucket buckets the absolute drop count, not the ratio: a nuisance row
// stays one row as the sheet grows, so its bucket stays put.
func dropBucket(dropped, kept int) string {
	switch {
	case kept == 0:
		return "all"
	case dropped <= 2:
		return fmt.Sprintf("%d", dropped)
	case dropped <= 5:
		return "3-5"
	case dropped <= 10:
		return "6-10"
	case dropped <= 20:
		return "11-20"
	case dropped <= 50:
		return "21-50"
	case dropped <= 100:
		return "51-100"
	default:
		return "100+"
	}
}

func doImport(im importer) {
	slog.Info("running importer", slog.String("importer", im.String()))

	// Setup sentry
	sentryTransport := sentry.NewHTTPSyncTransport()
	sentryTransport.Timeout = 3 * time.Second
	err := sentry.Init(sentry.ClientOptions{
		Release:     common.GitSHA,
		Environment: common.Config.Env,
		Transport:   sentryTransport,
		Tags: map[string]string{
			"command": "importer " + im.String(),
		},
	})
	if err != nil {
		utils.LogFatal("sentry.Init", slog.Any("err", err))
	}
	defer sentry.Flush(2 * time.Second)

	// do the thing
	if err := im.Init(); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("importer.Init", slog.Any("err", err))
	}

	if err := im.Import(); err != nil {
		sentry.CaptureException(err)
		utils.LogFatal("im.Import", slog.Any("err", err))
	}

	im.Close()

	slog.Info("importer completed", slog.String("importer", im.String()))
}

type BaseImporter struct {
	repo           repo.OrdersRepository
	eventEmitter   events.EventEmitter
	profileService profiles.ProfileService
}

func NewBaseImporter() *BaseImporter {
	return new(BaseImporter)
}

func (im *BaseImporter) Init() error {
	var err error

	im.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		return fmt.Errorf("events.CreateEmitter: %w", err)
	}

	im.repo, err = repo.NewOrdersDB(context.Background(), im.eventEmitter)
	if err != nil {
		return fmt.Errorf("repo.NewOrdersDB: %w", err)
	}

	im.profileService = profiles.NewProfileServiceAPI(keycloak.NewClient())

	return nil
}

func (im *BaseImporter) Close() {
	im.repo.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	im.eventEmitter.Close(ctx)
}

func (im *BaseImporter) getOrCreateAccount(ctx context.Context, email string) (int, error) {
	var account *repo.Account
	account, err := im.repo.GetAccount(ctx, 0, email)
	if err == nil {
		return account.ID, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("repo.GetAccount: %w", err)
	}

	slog.Info("account not found", slog.String("email", email))
	profile, err := im.profileService.LookupProfile(ctx, email)
	if err != nil {
		return 0, fmt.Errorf("profileService.LookupProfile: %w", err)
	}

	if profile == nil {
		return 0, errors.New("email not found in profile service")
	}

	slog.Info("creating new account", slog.String("email", email))
	account = &repo.Account{
		FirstName:   null.StringFromPtr(profile.FirstNameVernacular),
		LastName:    null.StringFromPtr(profile.LastNameVernacular),
		Email:       null.StringFromPtr(profile.PrimaryEmail),
		Phone:       null.StringFromPtr(profile.MobileNumber),
		Street:      null.StringFromPtr(profile.StreetAddress),
		City:        null.StringFromPtr(profile.City),
		State:       null.StringFromPtr(profile.StateOrRegion),
		Postcode:    null.StringFromPtr(profile.PostalCode),
		Country:     null.StringFromPtr(profile.Country),
		AccountType: null.StringFrom(common.AccountTypePersonal),
		UserKey:     null.StringFrom(profile.KeycloakID.String()),
	}

	// By key, not insert: the profile's keycloak id may already have an account
	// whose stored email is stale, and the key allows only one.
	account.ID, err = im.repo.GetOrCreateAccount(ctx, *account)
	if err != nil {
		return 0, fmt.Errorf("repo.GetOrCreateAccount: %w", err)
	}
	return account.ID, nil
}
