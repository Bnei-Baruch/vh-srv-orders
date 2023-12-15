package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
)

type OrdersRepository interface {
	CreateOrUpdateAccount(ctx context.Context, a Account) int
	CreateAccount(ctx context.Context, a Account) (int, error)
	GetAllAccounts(ctx context.Context, skip int, limit int, email string) (*[]Account, error)
	PatchAccount(ctx context.Context, req Account, accountID int) error
	SoftDeleteAccount(ctx context.Context, accountID int) error
	HardDeleteAllUserDataByAccountID(ctx context.Context, accountID int, kc_id string) error
	GetAccount(ctx context.Context, id int, email string) (Account, error)

	UpdateOrderStatusByOrderID(ctx context.Context, oid int, status string) error
	CreateOrderViaTransaction(ctx context.Context, req RequestOrder) (Order, error)
	SyncServiceRegistration(ctx context.Context, p Payment, order Order) error
	UpdateOrderAfterPayment(ctx context.Context, p Payment) (Order, error)
	GetOrderByID(ctx context.Context, orderID uint) Order
	GetPaymentForOrderID(ctx context.Context, orderID uint) Payment
	GetAccountForOrderID(ctx context.Context, orderID uint) Account
	ChargeOrdersToRenew(ctx context.Context, pmx string) int
	FlagDuplicateOrders(ctx context.Context, ProductType string) int
	FlagOrdersToRenew(ctx context.Context, month int64, year int64) int64
	CreateV2Order(ctx context.Context, order Order) (int, error)
	SoftDeleteOrderByID(ctx context.Context, orderID int) error
	PatchOrderByID(c context.Context, order Order, orderId int) error
	GetAllOrders(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time, productType string,
		currency string, status string, organisation string, email string, accountID int, evaluateMembership string,
		orderByPaymentDate string) (*[]Order, error)

	GetPaymentByID(ctx context.Context, id int) (Payment, error)
	SoftDeletePayment(c context.Context, paymentID int) error
	GetPaymentActivities(ctx context.Context, email string, productType string, paymentType string, skip int, limit int) ([]PaymentActivitiesRes, error)
	GetAllPayments(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time,
		paymentType string, paymentStatus string, orderType string, email string, accountID int,
		paymentsWithToken string, intOrderID int, orderByCreatedAt string) (*[]Payment, error)
	GetTotalParticipationStatusCount(ctx context.Context, email string, productType string,
		paymentType string) (int, error)
	GetPaymentByEmail(ctx context.Context, email string) ([]PaymentByEmail, error)
	GetOfflinePayments(ctx context.Context, skip int, limit int, method string, orderByCreatedAt string) ([]*OfflinePayment, error)
	FetchPaymentByParamX(ctx context.Context, paramX string) (PaymentWithFullName, error)
	CreatePayment(ctx context.Context, req RequestOrder, orderID int) (Payment, error)
	UpdatePayment(ctx context.Context, req RequestPaid) (Payment, error)
	UpdatePelecardPayment(c context.Context, req PaymentUpdate) error
	UpdateOfflinePayment(c context.Context, req PaymentUpdate) error
	UpdateHelpHavePayment(c context.Context, req PaymentUpdate) error
	UpdateParentPaymentTableStatusAndReturnOrderId(c context.Context, status string, paymentID int) (int, error)

	CountsAllOrders(ctx context.Context) int64
	CountsFilteredOrders(ctx context.Context, filter string) int64
	CountsTicketsOrders(ctx context.Context) int64
	CountsConventionOrders(ctx context.Context) int64
	CountsTickets10Orders(ctx context.Context) int64
	CountsTickets30Orders(ctx context.Context) int64
	PaidDetailCount(ctx context.Context) PaidDetailC

	GetCardDetailById(ctx context.Context, id int) (CardDetails, error)
	CreateCardDetailsAndGetId(ctx context.Context, p CardDetails) (int, error)
	SoftDeleteCardDetailById(c context.Context, id int) error
	PatchCardDetailsById(ctx context.Context, req CardDetails, id int) error
	GetAllCardDetails(ctx context.Context, skip int, limit int) (*[]CardDetails, error)

	GetTransactionById(ctx context.Context, id int) (Transaction, error)
	CreateTransactionAndGetId(ctx context.Context, p Transaction) (int, error)

	HardDeleteSpecialByEmail(ctx context.Context, email string) (error, int64)
	GetSpecialByEmail(ctx context.Context, email string) (Special, error)

	HasPaidMembership(ctx context.Context, email string) bool
	HasTicket(ctx context.Context, email string) bool
	HasSpecialMembership(ctx context.Context, email string) bool

	PerformOperation(ctx context.Context, req OperationReq) (int, error)
	RevertOperation(ctx context.Context, newEmail string, oldEmail string) error

	Close()
}

type OrdersDB struct {
	*pgxpool.Pool
	eventEmitter events.EventEmitter
}

func NewOrdersDB(ctx context.Context, eventEmitter events.EventEmitter) (OrdersRepository, error) {
	pool, err := pgxpool.Connect(ctx, GetDBURL())
	if err != nil {
		return nil, fmt.Errorf("pgxpool.Connect: %w", err)
	}
	return &OrdersDB{
		Pool:         pool,
		eventEmitter: eventEmitter,
	}, nil
}

func (o *OrdersDB) emitEvent(ctx context.Context, eventType string, payload map[string]interface{}) {
	builder := ctx.Value(common.CtxEventBuilder).(events.EventBuilder)
	event := builder.BuildEvent(eventType, payload)
	o.eventEmitter.Emit(event)
}
