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
	sheetValues, dropped, err := im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)), slog.Int("dropped", dropped))
	reportDroppedRows(im, dropped, len(sheetValues))

	newOrders := 0
	errOrders := 0
	for _, row := range sheetValues {
		// row.SheetRow, not the loop index: sheetValues is the *filtered*
		// slice, so with two rows dropped ahead of it the third survivor is
		// index 0 and sheet row 4.
		if err := im.createOrderAndPayments(row); err != nil {
			slog.Error("importer.createOrderAndPayments", slog.Int("row", row.SheetRow), slog.Any("err", err))
			errOrders++
			continue
		}
		newOrders++
	}
	// dropped is counted separately from with_errors: a row the parser threw
	// away never reaches createOrderAndPayments, so without this line it is in
	// no total and the summary adds up to fewer rows than the sheet holds.
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
	// SheetRow is the 1-based row in the spreadsheet this order came from, so
	// a failure at insert time can name the line an operator has to edit.
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

// parseGenericRows turns sheet rows into orders, skipping the header. Split out
// of getSheetValues, which builds its Sheets client inline and so cannot be
// reached from a test.
//
// Returns the orders it could read and the number of data rows it dropped. A
// dropped row is never fatal: reportDroppedRows raises its level when nothing
// survived, which says the same thing without killing a cron that will be
// handed the same sheet a minute later.
func parseGenericRows(values [][]any) ([]*GenericOrder, int) {
	orders := make([]*GenericOrder, 0)

	// An empty sheet has no header to skip, and values[1:] panics on it rather
	// than reporting an empty import.
	if len(values) == 0 {
		return orders, 0
	}

	dropped := 0
	for i, row := range values[1:] {
		// +2: i counts from the first data row, and the header is sheet row 1.
		sheetRow := i + 2

		// A spacer line between entries is not a malformed row; it is not a
		// row. Counting it would report a drop on every run of a sheet nobody
		// needs to fix.
		if blankRow(row) {
			continue
		}

		// The account this order is attached to is looked up by email, and
		// GetAccount with an empty one resolves to the most recently created
		// account that has no email — so a blank cell would silently bill the
		// order and its payment to an unrelated person. The specials parser
		// grew this guard last round; this parser reaches the same lookup
		// through getOrCreateAccount.
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

		if order.Currency != common.CurrencyUSD &&
			order.Currency != common.CurrencyEUR &&
			order.Currency != common.CurrencyNIS &&
			order.Currency != common.CurrencyRUR {
			// Was slog.Any("err", err) against the outer err from call.Do(),
			// which is nil by here — the value that failed is the useful thing.
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "currency"), slog.String("value", order.Currency))
			dropped++
			continue
		}

		var err error
		// 64: ParseFloat takes 32 or 64 and treats everything else as 64, so
		// the 10 that stood here parsed at 64 bits by accident. Amount is a
		// float64 all the way to the column.
		order.Amount, err = strconv.ParseFloat(cell(row, 1), 64)
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "amount"), slog.Any("err", err))
			dropped++
			continue
		}

		// 32, not Atoi: the destination is orders.quantity, which is int4.
		// Parsing at Go's int width accepts 4294967297 on a 64-bit build and
		// hands it to Postgres, which answers "4294967297 is greater than
		// maximum value for int4" and fails the row at insert time, one row at
		// a time, with the sheet already half imported. Rejecting it here makes
		// it a malformed row like any other.
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

	// Skipping a bad row keeps one broken cell from stopping the whole import.
	// A sheet where *every* row is bad is a different event: one changed date
	// format or one inserted column drops all of them, and the run then logs
	// count=0 with_errors=0 and exits 0 — indistinguishable from an empty
	// sheet, and silent in Sentry. Say so instead.
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
