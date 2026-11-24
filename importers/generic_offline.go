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

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
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
	sheetValues, dropped, err := im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)), slog.Int("dropped", dropped))
	reportDroppedRows(im, dropped, len(sheetValues))

	newOrders := 0
	errOrders := 0
	for _, row := range sheetValues {
		if err := im.createOrderAndPayments(row); err != nil {
			slog.Error("importer.createOrderAndPayments", slog.Int("row", row.SheetRow), slog.Any("err", err))
			errOrders++
			continue
		}
		newOrders++
	}
	slog.Info("import summary", slog.Int("new_orders", newOrders), slog.Int("with_errors", errOrders), slog.Int("dropped_by_parser", dropped))

	return nil
}

type GenericOrder struct {
	Email         string
	Amount        float64
	Currency      string
	Quantity      int
	Timestamp     time.Time
	PaymentMethod string
	Comment       string
	// SheetRow is the 1-based sheet row, for log lines an operator can act on.
	SheetRow int
}

func (im *GenericOfflineImporter) getSheetValues() ([]*GenericOrder, int, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, 0, fmt.Errorf("sheets.NewService: %w", err)
	}

	const spreadsheetId = "1jRygsoYqD_tUpEKXxVHY2_nAS52F3cdGp5spxFw8Uak"
	const spreadsheetRange = "import offline payments"
	call := sheetsService.Spreadsheets.Values.Get(spreadsheetId, spreadsheetRange)
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, 0, fmt.Errorf("sheetsService.Spreadsheets.Values.Get: %w", err)
	}

	orders, dropped := parseGenericRows(resp.Values)
	return orders, dropped, nil
}

// parseGenericRows turns sheet rows into orders, skipping the header. It
// returns the orders it could read and how many data rows it dropped; a bad
// row is logged and skipped, never fatal.
func parseGenericRows(values [][]any) ([]*GenericOrder, int) {
	orders := make([]*GenericOrder, 0)
	if len(values) == 0 {
		return orders, 0
	}

	dropped := 0
	for i, row := range values[1:] {
		sheetRow := i + 2 // the header is row 1
		if blankRow(row) {
			continue
		}

		// GetAccount with an empty email returns the newest account without
		// one, which would bill this order to an unrelated person.
		email := cell(row, 0)
		if email == "" {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "email"), slog.String("reason", "empty"))
			dropped++
			continue
		}

		order := &GenericOrder{
			SheetRow:      sheetRow,
			Email:         email,
			Currency:      cell(row, 2),
			PaymentMethod: cell(row, 5),
			Comment:       cell(row, 6),
		}

		if order.PaymentMethod == "" {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "payment_method"), slog.String("reason", "empty"))
			dropped++
			continue
		}

		if order.Currency != common.CurrencyUSD &&
			order.Currency != common.CurrencyEUR &&
			order.Currency != common.CurrencyNIS &&
			order.Currency != common.CurrencyRUR {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "currency"), slog.String("value", order.Currency))
			dropped++
			continue
		}

		var err error
		order.Amount, err = strconv.ParseFloat(cell(row, 1), 64)
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "amount"), slog.Any("err", err))
			dropped++
			continue
		}

		// 32 bits: orders.quantity is int4, so a larger value would fail at
		// insert time instead of here.
		quantity, err := strconv.ParseInt(cell(row, 3), 10, 32)
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "quantity"), slog.Any("err", err))
			dropped++
			continue
		}
		order.Quantity = int(quantity)

		order.Timestamp, err = time.Parse(time.DateTime, cell(row, 4))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "timestamp"), slog.Any("err", err))
			dropped++
			continue
		}

		orders = append(orders, order)
	}
	return orders, dropped
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
		Quantity:      null.IntFrom(rOrder.Quantity),
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
