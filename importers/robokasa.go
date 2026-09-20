package importers

import (
	"context"
	"encoding/json"
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

func ImportRobokasa() {
	doImport(NewRobokasaImporter())
}

type RobokasaImporter struct {
	BaseImporter
}

func NewRobokasaImporter() *RobokasaImporter {
	return new(RobokasaImporter)
}

func (im *RobokasaImporter) String() string {
	return "robokasa"
}

// Import fetches all robokasa orders and import the ones we don't have.
// Idempotency is guaranteed by robokasa order_id being stored in our DB as well.
func (im *RobokasaImporter) Import() error {
	sheetValues, dropped, err := im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)), slog.Int("dropped", dropped))
	reportDroppedRows(im, dropped, len(sheetValues))

	var existingPayments map[string]*repo.OfflinePayment
	existingPayments, err = im.getExistingPayments()
	if err != nil {
		return fmt.Errorf("importer.getExistingPayments: %w", err)
	}
	slog.Info("importer.getExistingPayments", slog.Int("count", len(existingPayments)))

	newOrders := 0
	skippedOrders := 0
	errOrders := 0
	for i, row := range sheetValues {
		if _, ok := existingPayments[row.OrderID]; ok {
			skippedOrders++
			continue
		}

		if err := im.createOrderAndPayments(row); err != nil {
			slog.Error("importer.createOrderAndPayments", slog.Int("line", i+1), slog.String("robokasa_id", row.OrderID), slog.Any("err", err))
			errOrders++
			continue
		}
		newOrders++
	}
	slog.Info("import summary", slog.Int("new_orders", newOrders), slog.Int("skipped_orders", skippedOrders), slog.Int("with_errors", errOrders))

	return nil
}

type RobokasaOrder struct {
	OrderID   string
	Email     string
	Amount    float64
	Timestamp time.Time
}

func (im *RobokasaImporter) getSheetValues() ([]*RobokasaOrder, int, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, 0, fmt.Errorf("sheets.NewService: %w", err)
	}

	call := sheetsService.Spreadsheets.Values.Get("1w2kn2rHKMp63lcmYZcWZwEiEW5AgcwS6eGdyNEghNZU", "ArvutRus")
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, 0, fmt.Errorf("sheetsService.Spreadsheets.Values.Get: %w", err)
	}

	orders, dropped := parseRobokasaRows(resp.Values)
	return orders, dropped, nil
}

// parseRobokasaRows turns sheet rows into orders. Split out of getSheetValues,
// which builds its Sheets client inline and so cannot be reached from a test.
//
// This export has no header row, so sheet row 1 is the first order.
func parseRobokasaRows(values [][]any) ([]*RobokasaOrder, int) {
	orders := make([]*RobokasaOrder, 0)

	dropped := 0
	for i, row := range values {
		sheetRow := i + 1

		if blankRow(row) {
			continue
		}

		// cell(), not row[n].(string): this parser had both hazards
		// sheet_row.go describes. A trailing blank shortens the row, so an
		// export with an empty timestamp column gives len(row) == 3 and row[3]
		// panics; and a cell the sheet stores as a number arrives as float64,
		// where the assertion panics too. Either one killed the whole run.
		order := &RobokasaOrder{
			OrderID: cell(row, 0),
			Email:   cell(row, 1),
		}

		var err error
		order.Amount, err = strconv.ParseFloat(cell(row, 2), 64)
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "amount"), slog.Any("err", err))
			dropped++
			continue
		}

		order.Timestamp, err = time.Parse(time.DateTime, cell(row, 3))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "timestamp"), slog.Any("err", err))
			dropped++
			continue
		}

		orders = append(orders, order)
	}

	return orders, dropped
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
				slog.Warn("missing offline payment properties", slog.Int("payment_id", payment.ID))
				continue
			}

			var props map[string]interface{}
			if err := payment.Properties.Unmarshal(&props); err != nil {
				slog.Warn("json.Unmarshal offline payment properties", slog.Int("payment_id", payment.ID), slog.Any("err", err))
				continue
			}

			rID, ok := props[common.OfflinePaymentPropertiesRobokasaID]
			if !ok {
				slog.Warn("offline payment missing robokasa_id", slog.Int("payment_id", payment.ID))
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
	event.Actor = events.ActorSystem
	return event
}
