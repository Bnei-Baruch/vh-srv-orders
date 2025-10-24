package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
)

type OrdersRepository interface {
	GetAccount(ctx context.Context, id int, email string) (*Account, error)
	GetAllAccounts(ctx context.Context, skip int, limit int, email string) ([]Account, error)
	GetAccountIDByKeycloakID(ctx context.Context, keycloakId string) (int, error)
	GetEmailByKeycloakID(ctx context.Context, keycloakId string) (string, error)
	CreateAccount(ctx context.Context, a Account) (int, error)
	GetOrCreateAccount(ctx context.Context, a Account) (int, error)
	GetOrCreateAccountFromProfile(ctx context.Context, keycloakID string) (int, error)
	PatchAccount(ctx context.Context, req Account, accountID int) error
	PatchOrCreateAccount(ctx context.Context, a Account) (int, error)
	SoftDeleteAccount(ctx context.Context, accountID int) error
	HardDeleteAllUserDataByAccountID(ctx context.Context, accountID int, kc_id string) error
	MergeAccountsOrders(ctx context.Context, request AccountMergeRequest) error

	UpdateOrderStatusByOrderID(ctx context.Context, oid int, status string) error
	CreateOrderViaTransaction(ctx context.Context, req RequestOrder) (*Order, error)
	UpdateOrderAfterPayment(ctx context.Context, p Payment) error
	GetOrderByID(ctx context.Context, orderID uint) (*Order, error)
	GetPaymentForOrderID(ctx context.Context, orderID uint) (*Payment, error)
	GetAccountForOrderID(ctx context.Context, orderID uint) (*Account, error)
	ChargeOrdersToRenew(ctx context.Context, pmx string) (int, error)
	FlagDuplicateOrders(ctx context.Context, ProductType string) (int, error)
	FlagOrdersToRenew(ctx context.Context, month int64, year int64) (int64, error)
	UpdateOrdersToken(ctx context.Context, req RequestUpdateToken) error

	CreateV2Order(ctx context.Context, order Order) (int, error)
	SoftDeleteOrderByID(ctx context.Context, orderID int) error
	PatchOrderByID(c context.Context, order Order, orderId int) error
	GetAllOrders(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time, productType string,
		currency string, status string, organisation string, email string, accountID int, keycloakID string, evaluateMembership string,
		orderByPaymentDate string) (*[]Order, error)

	GetPaymentByID(ctx context.Context, id int) (*Payment, error)
	SoftDeletePayment(c context.Context, paymentID int) error
	GetPaymentActivities(ctx context.Context, email string, productType string, paymentType string, skip int, limit int) ([]PaymentActivitiesRes, error)
	GetAllPayments(ctx context.Context, skip int, limit int, fromDate string, toDate *time.Time,
		paymentType string, paymentStatus string, orderType string, email string, accountID int,
		paymentsWithToken string, intOrderID int, orderByCreatedAt string) ([]Payment, error)
	GetTotalParticipationStatusCount(ctx context.Context, email string, productType string,
		paymentType string) (int, error)
	GetPaymentByEmail(ctx context.Context, email string) ([]PaymentByEmail, error)
	GetOfflinePayments(ctx context.Context, skip int, limit int, method string, orderByCreatedAt string) ([]*OfflinePayment, error)
	FetchPaymentByParamX(ctx context.Context, paramX string) (*PaymentWithFullName, error)
	CreatePayment(ctx context.Context, req RequestOrder, orderID int) (*Payment, error)
	UpdatePayment(ctx context.Context, req RequestPaid) (*Payment, error)
	UpdatePelecardPayment(c context.Context, req PaymentUpdate) error
	UpdateOfflinePayment(c context.Context, req PaymentUpdate) error
	UpdateHelpHavePayment(c context.Context, req PaymentUpdate) error
	UpdateParentPaymentTableStatusAndReturnOrderId(c context.Context, status string, paymentID int) (int, error)

	CountsAllOrders(ctx context.Context) (int64, error)
	CountsFilteredOrders(ctx context.Context, filter string) (int64, error)
	CountsTicketsOrders(ctx context.Context) (int64, error)
	CountsConventionOrders(ctx context.Context) (int64, error)
	CountsTickets10Orders(ctx context.Context) (int64, error)
	CountsTickets30Orders(ctx context.Context) (int64, error)
	PaidDetailCount(ctx context.Context) (*PaidDetailC, error)

	GetCardDetailById(ctx context.Context, id int) (*CardDetails, error)
	CreateCardDetailsAndGetId(ctx context.Context, p CardDetails) (int, error)
	SoftDeleteCardDetailById(c context.Context, id int) error
	PatchCardDetailsById(ctx context.Context, req CardDetails, id int) error
	GetAllCardDetails(ctx context.Context, skip int, limit int) ([]CardDetails, error)

	GetTransactionById(ctx context.Context, id int, accountId *int) (*Transaction, error)
	CreateTransactionAndGetId(ctx context.Context, p Transaction) (int, error)

	CreateSpecial(ctx context.Context, s Special) (int, error)
	DeleteSpecialById(ctx context.Context, id int) error
	DeleteSpecialsByKeycloakId(ctx context.Context, keycloakID string) error
	GetAllSpecialsByEmail(ctx context.Context, email string) ([]*Special, error)
	GetUniqueEmailsFromSpecial(ctx context.Context) ([]string, error)
	GetSpecialsByKeycloakId(ctx context.Context, keycloakID string) ([]*Special, error)
	GetSpecialsById(ctx context.Context, id string) ([]*Special, error)
	GetAllSpecials(ctx context.Context) ([]*Special, error)
	SetKeycloakIdByEmail(ctx context.Context, email string, keycloakID string) error

	HasPaidMembership(ctx context.Context, email string) (bool, error)
	HasTicket(ctx context.Context, email string) (bool, error)
	HasSpecialMembership(ctx context.Context, email string) (bool, error)

	PerformOperation(ctx context.Context, req OperationReq) (int, error)
	RevertOperation(ctx context.Context, newEmail string, oldEmail string) error
	IsSubjectID(ctx context.Context, keycloakID, userID string) (bool, error)

	GetMonthlyPriceByKCID(ctx context.Context, keycloakID string) (*UserMonthlyPriceRes, error)

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
