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

// reportDroppedRows tells Sentry about rows the parser threw away.
//
// Before the parsers learned to skip, one unparseable cell returned an error
// from getSheetValues and doImport turned that into CaptureException plus a
// non-zero exit. Skipping is better per row and worse per sheet: 30 rows
// dropped out of 200 because someone switched the date column to DD/MM/YYYY
// leaves 170 imported, exit 0, and the only trace a log line nobody reads.
// Those 30 people silently never get their specials. A drop is a Sentry event
// at any ratio.
//
// Nothing survived is the same event at a higher level rather than a different
// mechanism. It used to abort the import, which read "the sheet format
// changed" off a condition that two operators typing GBP also satisfy — and
// since the sheet is not cleared between runs, the cron then exited 1 on every
// invocation until someone edited it. The distinction is worth an alert level,
// not a dead importer.
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
	// Grouped by a bucketed drop count, not by the exact counts and not by the
	// importer alone.
	//
	// The exact counts fragment: the sheets are never cleared, so the one row
	// nobody will fix is dropped again every tick, and `1 of 200`, `1 of 201`,
	// `1 of 202` open three issues as the sheet grows. The importer alone
	// over-groups the other way: archiving that permanent nuisance would
	// archive the break that means the date column just changed format.
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)
		scope.SetFingerprint([]string{"importer-dropped-rows", im.String(), dropBucket(dropped, kept)})
		sentry.CaptureMessage(message)
	})
}

// dropBucket names how much of the sheet was thrown away, coarsely enough that
// the same recurring problem keeps one Sentry issue.
//
// On the absolute count, not the ratio. The nuisance this groups is a fixed
// small number of rows — a totals line, one half-typed entry — and it stays
// that number while the sheet grows around it, so an absolute bucket is what is
// actually stable. A ratio moves with the denominator: one bad row of 10 and
// one bad row of 11 fell in different buckets and opened a new issue every time
// the sheet grew, and 10 bad rows of 200 shared a bucket with the permanent
// single-row nuisance, so archiving that would have archived a break denying
// specials to ten people.
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

	account.ID, err = im.repo.CreateAccount(ctx, *account)
	if err != nil {
		return 0, fmt.Errorf("repo.CreateAccount: %w", err)
	}
	return account.ID, nil
}
