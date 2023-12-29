package importers

import (
	"context"
	"encoding/json"
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

func ImportRobokasa() {
	importer := NewRobokasaImporter()

	if err := importer.Init(); err != nil {
		log.Fatal("importer.Init", err)
	}

	if err := importer.Import(); err != nil {
		log.Fatal("importer.Import", err)
	}

	importer.Close()
}

type RobokasaImporter struct {
	repo           repo.OrdersRepository
	eventEmitter   events.EventEmitter
	profileService profiles.ProfileService
}

func NewRobokasaImporter() *RobokasaImporter {
	return new(RobokasaImporter)
}

func (im *RobokasaImporter) Init() error {
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

func (im *RobokasaImporter) Close() {
	im.repo.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	im.eventEmitter.Close(ctx)
}

// Import fetches all robokasa orders and import the ones we don't have.
// Idempotency is guaranteed by robokasa order_id being stored in our DB as well.
func (im *RobokasaImporter) Import() error {
	log.Println("Importing payments from robokasa")

	var err error

	log.Println("Getting all rows from spreadsheet")
	var sheetValues []*RobokasaOrder
	sheetValues, err = im.getSheetValues()
	if err != nil {
		return fmt.Errorf("getting sheet values: %w", err)
	}
	log.Printf("Got %d rows from spreadsheet\n", len(sheetValues))

	log.Println("Getting existing payments from db")
	var existingPayments map[string]*repo.OfflinePayment
	existingPayments, err = im.getExistingPayments()
	if err != nil {
		return fmt.Errorf("getting existing payments: %w", err)
	}
	log.Printf("Got %d existing payments from db\n", len(existingPayments))

	log.Println("Creating new orders")
	newOrders := 0
	skippedOrders := 0
	errOrders := 0
	for _, row := range sheetValues {
		if _, ok := existingPayments[row.OrderID]; ok {
			skippedOrders++
			continue
		}

		if err := im.createOrderAndPayments(row); err != nil {
			log.Printf("WARNING: error creating order and payments [robokasa_id: %s]: %v\n", row.OrderID, err)
			errOrders++
			continue
		}
		newOrders++
	}
	log.Printf("Created %d new orders, skipped %d existing. %d had errors \n", newOrders, skippedOrders, errOrders)

	return nil
}

type RobokasaOrder struct {
	OrderID   string
	Email     string
	Amount    float64
	Timestamp time.Time
}

func (im *RobokasaImporter) getSheetValues() ([]*RobokasaOrder, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("initialize google sheets service: %w", err)
	}

	call := sheetsService.Spreadsheets.Values.Get("1w2kn2rHKMp63lcmYZcWZwEiEW5AgcwS6eGdyNEghNZU", "ArvutRus")
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("get sheet values: %w", err)
	}

	orders := make([]*RobokasaOrder, 0)
	for i, row := range resp.Values {
		order := &RobokasaOrder{
			OrderID: row[0].(string),
			Email:   row[1].(string),
		}

		var err error
		order.Amount, err = strconv.ParseFloat(row[2].(string), 10)
		if err != nil {
			log.Printf("WARNING: malformed row %d [amount]: %v\n", i+1, err)
			continue
		}

		order.Timestamp, err = time.Parse(time.DateTime, row[3].(string))
		if err != nil {
			log.Printf("WARNING: malformed row %d [timestamp]: %v\n", i+1, err)
			continue
		}

		orders = append(orders, order)
	}

	return orders, nil
}

func (im *RobokasaImporter) getExistingPayments() (map[string]*repo.OfflinePayment, error) {
	pageSize := 1000
	page := 0
	byRobokasaID := make(map[string]*repo.OfflinePayment)
	for {
		payments, err := im.repo.GetOfflinePayments(context.Background(),
			page*pageSize, pageSize, common.OfflinePaymentMethodRobokasa, "asc")
		if err != nil {
			return nil, fmt.Errorf("repo.GetOfflinePayments [page %d]: %w", page, err)
		}

		for _, payment := range payments {
			if !payment.Properties.Valid {
				log.Printf("WARNING: missing offline payment properties [id: %d]\n", payment.ID)
				continue
			}

			var props map[string]interface{}
			if err := payment.Properties.Unmarshal(&props); err != nil {
				log.Printf("WARNING: json.Unmarshal offline payment properties [id: %d]: %v\n", payment.ID, err)
				continue
			}

			rID, ok := props[common.OfflinePaymentPropertiesRobokasaID]
			if !ok {
				log.Printf("WARNING: offline payment missing robokasa_id [id: %d]\n", payment.ID)
				continue
			}

			byRobokasaID[rID.(string)] = payment
		}

		page++
		if len(payments) < pageSize {
			break
		}
	}

	return byRobokasaID, nil
}

// createOrderAndPayments will create a fresh Order, Payment and OfflinePayment for the given robokasa order
func (im *RobokasaImporter) createOrderAndPayments(rOrder *RobokasaOrder) error {
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

func (im *RobokasaImporter) getOrCreateAccount(ctx context.Context, email string) (int, error) {
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

func (im *RobokasaImporter) createOrder(ctx context.Context, rOrder *RobokasaOrder, accountID int) (*repo.Order, error) {
	order := repo.Order{
		Type:          null.StringFrom(common.OrderTypeRegular),
		ProductType:   null.StringFrom(common.ProductTypeGlobalMembership),
		AccountID:     null.IntFrom(accountID),
		Amount:        null.Float64From(rOrder.Amount),
		Currency:      null.StringFrom(common.CurrencyRUR),
		SKU:           null.StringFrom(common.ProductSKU40037),
		Status:        null.StringFrom(common.OrderStatusPaid),
		OrderLanguage: null.StringFrom(common.OrderLanguageRussian),
		PaymentDate:   null.TimeFrom(rOrder.Timestamp),
		Quantity:      null.IntFrom(1),
	}

	var err error
	order.ID, err = im.repo.CreateV2Order(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("repo.CreateV2Order: %w", err)
	}

	return &order, nil
}

func (im *RobokasaImporter) createPayment(ctx context.Context, rOrder *RobokasaOrder, order *repo.Order) error {
	propsB, _ := json.Marshal(map[string]interface{}{
		common.OfflinePaymentPropertiesRobokasaID: rOrder.OrderID,
	})

	req := repo.RequestOrder{
		Amount:               order.Amount,
		Currency:             order.Currency,
		PaymentType:          null.StringFrom(common.PaymentTypeOffline),
		PaymentStatus:        null.StringFrom(common.PaymentStatusSuccess),
		PaymentMethod:        null.StringFrom(common.OfflinePaymentMethodRobokasa),
		OfflinePaymentStatus: null.StringFrom(common.PaymentStatusSuccess),
		Properties:           null.JSONFrom(propsB),
	}

	_, err := im.repo.CreatePayment(ctx, req, order.ID)
	if err != nil {
		return fmt.Errorf("repo.CreatePayment: %w", err)
	}

	return nil
}

func (im *RobokasaImporter) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentRobokasaImporter
	return event
}
