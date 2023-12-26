package importers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v4"
	"github.com/volatiletech/null/v9"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func ImportGeneric() {
	importer := NewGenericOfflineImporter()

	if err := importer.Init(); err != nil {
		log.Fatal("importer.Init", err)
	}

	if err := importer.Import(); err != nil {
		log.Fatal("importer.Import", err)
	}

	importer.Close()
}

type GenericOfflineImporter struct {
	repo           repo.OrdersRepository
	eventEmitter   events.EventEmitter
	profileService profiles.ProfileService
}

func NewGenericOfflineImporter() *GenericOfflineImporter {
	return new(GenericOfflineImporter)
}

func (im *GenericOfflineImporter) Init() error {
	var err error

	im.eventEmitter, err = events.CreateEmitter()
	if err != nil {
		log.Fatalf("Error creating events emitter: %v\n", err)
	}

	im.repo, err = repo.NewOrdersDB(context.Background(), im.eventEmitter)
	if err != nil {
		return fmt.Errorf("repo.NewOrdersDB: %v", err)
	}

	im.profileService = profiles.NewProfileServiceAPI(keycloak.NewClient())

	return nil
}

func (im *GenericOfflineImporter) Close() {
	im.repo.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	im.eventEmitter.Close(ctx)
}

// Import fetches all orders and import them.
// No idempotency is guaranteed, user carefully.
func (im *GenericOfflineImporter) Import() error {
	log.Println("Importing payments")

	var err error

	log.Println("Getting all rows from spreadsheet")
	var sheetValues []*GenericOrder
	sheetValues, err = im.getSheetValues()
	if err != nil {
		return fmt.Errorf("getting sheet values: %w", err)
	}
	log.Printf("Got %d rows from spreadsheet\n", len(sheetValues))

	log.Println("Creating new orders")
	newOrders := 0
	errOrders := 0
	for i, row := range sheetValues {
		if err := im.createOrderAndPayments(row); err != nil {
			log.Printf("WARNING: error creating order and payments [row %d]: %v\n", i, err)
			errOrders++
			continue
		}
		newOrders++
	}
	log.Printf("Created %d new orders. %d had errors \n", newOrders, errOrders)

	return nil
}

type GenericOrder struct {
	Email         string
	Amount        float64
	Currency      string
	Quantity      int64
	Timestamp     time.Time
	PaymentMethod string
	Comment       string
}

func (im *GenericOfflineImporter) getSheetValues() ([]*GenericOrder, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("initialize google sheets service: %w", err)
	}

	const spreadsheetId = "1jRygsoYqD_tUpEKXxVHY2_nAS52F3cdGp5spxFw8Uak"
	const spreadsheetRange = "import offline payments"
	call := sheetsService.Spreadsheets.Values.Get(spreadsheetId, spreadsheetRange)
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("get sheet values: %w", err)
	}

	orders := make([]*GenericOrder, 0)
	for i, row := range resp.Values[1:] {
		order := &GenericOrder{
			Email:         row[0].(string),
			Currency:      row[2].(string),
			PaymentMethod: row[5].(string),
			Comment:       row[6].(string),
		}

		if order.Currency != common.CurrencyUSD &&
			order.Currency != common.CurrencyEUR &&
			order.Currency != common.CurrencyNIS &&
			order.Currency != common.CurrencyRUR {
			log.Printf("WARNING: malformed row %d [currency]: %v\n", i+1, err)
			continue
		}

		var err error
		order.Amount, err = strconv.ParseFloat(row[1].(string), 10)
		if err != nil {
			log.Printf("WARNING: malformed row %d [amount]: %v\n", i+1, err)
			continue
		}

		order.Quantity, err = strconv.ParseInt(row[3].(string), 10, 64)
		if err != nil {
			log.Printf("WARNING: malformed row %d [quantity]: %v\n", i+1, err)
			continue
		}

		order.Timestamp, err = time.Parse(time.DateTime, row[4].(string))
		if err != nil {
			log.Printf("WARNING: malformed row %d [timestamp]: %v\n", i+1, err)
			continue
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// createOrderAndPayments will create a fresh Order, Payment and OfflinePayment for the given order
func (im *GenericOfflineImporter) createOrderAndPayments(rOrder *GenericOrder) error {
	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, im)

	accountID, err := im.getOrCreateAccount(ctx, rOrder.Email)
	if err != nil {
		return fmt.Errorf("get or create account: %w", err)
	}

	var order *repo.Order
	order, err = im.createOrder(ctx, rOrder, accountID)
	if err != nil {
		return fmt.Errorf("create order: %w", err)
	}

	err = im.createPayment(ctx, rOrder, order)
	if err != nil {
		return fmt.Errorf("create payment: %w", err)
	}

	return nil
}

func (im *GenericOfflineImporter) getOrCreateAccount(ctx context.Context, email string) (int, error) {
	var account repo.Account
	account, err := im.repo.GetAccount(ctx, 0, email)
	if err == nil {
		return account.ID, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("get account: %w", err)
	}

	log.Printf("INFO: account not found for %s\n", email)
	profile, err := im.profileService.LookupProfile(ctx, email)
	if err != nil {
		return 0, fmt.Errorf("lookup profile by email: %w", err)
	}

	if profile == nil {
		return 0, errors.New("email not found in profile service")
	}

	log.Printf("INFO: found profile, creating account for %s\n", email)
	account = repo.Account{
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

	account.ID, err = im.repo.CreateAccount(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("repo.CreateAccount: %w", err)
	}
	return account.ID, nil
}

func (im *GenericOfflineImporter) createOrder(ctx context.Context, rOrder *GenericOrder, accountID int) (*repo.Order, error) {
	order := repo.Order{
		Type:          null.StringFrom(common.OrderTypeRegular),
		ProductType:   null.StringFrom(common.ProductTypeGlobalMembership),
		AccountID:     null.IntFrom(accountID),
		Amount:        null.Float64From(rOrder.Amount),
		Currency:      null.StringFrom(rOrder.Currency),
		SKU:           null.StringFrom(common.ProductSKU40037),
		Status:        null.StringFrom(common.OrderStatusPaid),
		OrderLanguage: null.StringFrom(common.OrderLanguageEnglish),
		PaymentDate:   null.TimeFrom(rOrder.Timestamp),
		Quantity:      null.IntFrom(int(rOrder.Quantity)),
	}

	var err error
	order.ID, err = im.repo.CreateV2Order(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("repo.CreateV2Order: %w", err)
	}

	return &order, nil
}

func (im *GenericOfflineImporter) createPayment(ctx context.Context, rOrder *GenericOrder, order *repo.Order) error {
	req := repo.RequestOrder{
		Amount:               order.Amount,
		PaymentType:          null.StringFrom(common.PaymentTypeOffline),
		PaymentStatus:        null.StringFrom(common.PaymentStatusSuccess),
		PaymentMethod:        null.StringFrom(rOrder.PaymentMethod),
		OfflinePaymentStatus: null.StringFrom(common.PaymentStatusSuccess),
		ExtraInfo:            null.StringFrom(rOrder.Comment),
	}

	_, err := im.repo.CreatePayment(ctx, req, order.ID)
	if err != nil {
		return fmt.Errorf("repo.CreatePayment: %w", err)
	}

	return nil
}

func (im *GenericOfflineImporter) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentGenericImporter
	return event
}
