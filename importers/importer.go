package importers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type importer interface {
	String() string
	Init() error
	Close()
	Import() error
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
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
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

	account.ID, err = im.repo.CreateAccount(ctx, *account)
	if err != nil {
		return 0, fmt.Errorf("repo.CreateAccount: %w", err)
	}
	return account.ID, nil
}
