package importers

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/volatiletech/null/v9"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func ImportGeneric() {
	doImport(NewGenericOfflineImporter())
}

type GenericOfflineImporter struct {
	BaseImporter
}

func NewGenericOfflineImporter() *GenericOfflineImporter {
	return new(GenericOfflineImporter)
}

func (im *GenericOfflineImporter) String() string {
	return "generic offline"
}

// Import fetches all orders and import them.
// No idempotency is guaranteed, use carefully.
func (im *GenericOfflineImporter) Import() error {
	var err error

	var sheetValues []*GenericOrder
	sheetValues, err = im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)))

	newOrders := 0
	errOrders := 0
	for i, row := range sheetValues {
		if err := im.createOrderAndPayments(row); err != nil {
			slog.Error("importer.createOrderAndPayments", slog.Int("line", i+1), slog.Any("err", err))
			errOrders++
			continue
		}
		newOrders++
	}
	slog.Info("import summary", slog.Int("new_orders", newOrders), slog.Int("with_errors", errOrders))

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
		return nil, fmt.Errorf("sheets.NewService: %w", err)
	}

	const spreadsheetId = "1jRygsoYqD_tUpEKXxVHY2_nAS52F3cdGp5spxFw8Uak"
	const spreadsheetRange = "import offline payments"
	call := sheetsService.Spreadsheets.Values.Get(spreadsheetId, spreadsheetRange)
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("sheetsService.Spreadsheets.Values.Get: %w", err)
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
			slog.Warn("malformed row", slog.Int("row", i+1), slog.String("column", "currency"), slog.Any("err", err))
			continue
		}

		var err error
		order.Amount, err = strconv.ParseFloat(row[1].(string), 10)
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", i+1), slog.String("column", "amount"), slog.Any("err", err))
			continue
		}

		order.Quantity, err = strconv.ParseInt(row[3].(string), 10, 64)
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", i+1), slog.String("column", "quantity"), slog.Any("err", err))
			continue
		}

		order.Timestamp, err = time.Parse(time.DateTime, row[4].(string))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", i+1), slog.String("column", "timestamp"), slog.Any("err", err))
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
		return fmt.Errorf("importer.getOrCreateAccount: %w", err)
	}

	var order *repo.Order
	order, err = im.createOrder(ctx, rOrder, accountID)
	if err != nil {
		return fmt.Errorf("importer.createOrder: %w", err)
	}

	err = im.createPayment(ctx, rOrder, order)
	if err != nil {
		return fmt.Errorf("importer.createPayment: %w", err)
	}

	return nil
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
		Currency:             order.Currency,
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
	event.Component = events.ComponentOfflinePaymentsImporter
	event.Actor = events.ActorSystem
	return event
}
