package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
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

type OrdersDB struct {
	*pgxpool.Pool
	eventEmitter   events.EventEmitter
	profileService profiles.ProfileService
}

func NewOrdersDB(ctx context.Context, eventEmitter events.EventEmitter) (*OrdersDB, error) {
	return NewOrdersDBUrl(ctx, GetDBURL(), eventEmitter)
}

func NewOrdersDBUrl(ctx context.Context, db_url string, eventEmitter events.EventEmitter) (*OrdersDB, error) {
	pool, err := pgxpool.Connect(ctx, db_url)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.Connect: %w", err)
	}
	return &OrdersDB{
		Pool:           pool,
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
