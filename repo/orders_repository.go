package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

// OrdersRepository is what domain/billing, cmd and importers call. They take
// it whole because they mock it — that is the only reason it is an interface
// rather than *OrdersDB, and the reason it is this size rather than smaller.
//
// It is not the place for a method only api or a narrow consumer interface
// calls; *OrdersDB carries those, and each consumer names what it uses. A
// method added here without one of the three above calling it lands in every
// one of their mocks for nothing.
type OrdersRepository interface {
	GetAccount(ctx context.Context, id int, email string) (*Account, error)
	CreateAccount(ctx context.Context, a Account) (int, error)

	LoadRenewalData(ctx context.Context, orderID uint) (*RenewalData, error)
	CreateRenewalPayment(ctx context.Context, data *RenewalData, amount float64, currency, pricingVersion string, pricingEvaluation null.JSON, pmx string) (*Payment, error)
	FinalizeRenewal(ctx context.Context, orderID uint, payment *Payment) error
	FlagOrdersToRenew(ctx context.Context, month int64, year int64) (int64, error)
	FlagOrder(ctx context.Context, id int, flag string) error
	GetFlaggedOrders(ctx context.Context) ([]Order, error)
	GetOrderIDsToRenew(ctx context.Context) ([]uint, error)
	GetOrderIDsWithPricingError(ctx context.Context) ([]uint, error)
	MarkResolvedForRenew(ctx context.Context, orderIDs []uint) error
	GetTokensForOrders(ctx context.Context, orderIDs []int) (map[int]string, error)
	ClearAllFlags(ctx context.Context) error
	UpdateOrdersUserKeyFromAccounts(ctx context.Context) error
	GetOrdersToSkipDouble(ctx context.Context, year, month int, lastDay time.Time) ([]string, error)
	GetOrdersToSkipFresh(ctx context.Context, year, month int, lastDay time.Time) ([]string, error)
	SkipOrdersByUserKey(ctx context.Context, userkey string) (int, error)

	CreateV2Order(ctx context.Context, order Order) (int, error)

	GetPaymentByID(ctx context.Context, id int) (*Payment, error)
	GetOfflinePayments(ctx context.Context, skip int, limit int, method string, orderByCreatedAt string) ([]*OfflinePayment, error)
	CreatePayment(ctx context.Context, req RequestOrder, orderID int) (*Payment, error)

	CreateSpecial(ctx context.Context, s Special) (int, error)
	GetAllSpecialsByEmail(ctx context.Context, email string) ([]*Special, error)
	GetUniqueEmailsFromSpecial(ctx context.Context) ([]string, error)

	Close()
}

// OrdersDB owns the connection pool in a field rather than embedding it.
//
// Embedding promoted Exec, Query, QueryRow, Begin and the rest onto *OrdersDB,
// so every holder of the concrete type could reach the database directly and
// skip the repo layer — which is where emitEvent lives, so such a write lands
// with no event and nothing downstream hears about it. While api.repo was an
// interface the compiler hid those methods; once it became concrete the only
// thing standing between a handler and raw SQL was an AST test that could not
// prove absence. A named field restores that barrier to the compiler.
type OrdersDB struct {
	pool           *pgxpool.Pool
	eventEmitter   events.EventEmitter
	profileService profiles.ProfileService
}

// Close releases the pool. Explicit because the pool is no longer embedded, and
// the app, the importers and the workers all shut down through it.
func (o *OrdersDB) Close() { o.pool.Close() }

func NewOrdersDB(ctx context.Context, eventEmitter events.EventEmitter) (*OrdersDB, error) {
	return NewOrdersDBUrl(ctx, GetDBURL(), eventEmitter)
}

func NewOrdersDBUrl(ctx context.Context, db_url string, eventEmitter events.EventEmitter) (*OrdersDB, error) {
	pool, err := pgxpool.New(ctx, db_url)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	// v4's pgxpool.Connect dialled before returning; v5's New only parses the
	// DSN and leaves connecting to a background goroutine, so without this the
	// constructor succeeds against a dead database and every caller's error
	// check is dead code. Close on failure, or the pool's health-check
	// goroutine outlives the failed construction.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pool.Ping: %w", err)
	}
	return &OrdersDB{
		pool:           pool,
		eventEmitter:   eventEmitter,
		profileService: profiles.NewProfileServiceAPI(keycloak.NewClient()),
	}, nil
}

func (o *OrdersDB) SetProfileService(ps profiles.ProfileService) {
	o.profileService = ps
}

func (o *OrdersDB) emitEvent(ctx context.Context, eventType string, payload map[string]interface{}) {
	builder := ctx.Value(common.CtxEventBuilder).(events.EventBuilder)
	event := builder.BuildEvent(eventType, payload)
	o.eventEmitter.Emit(ctx, event)
}
