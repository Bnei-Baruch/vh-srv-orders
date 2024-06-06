package importers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/volatiletech/null/v9"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func ImportSpecials() {
	doImport(NewSpecialsImporter())
}

type SpecialsImporter struct {
	BaseImporter
}

func NewSpecialsImporter() *SpecialsImporter {
	return new(SpecialsImporter)
}

func (im *SpecialsImporter) String() string {
	return "importer specials"
}

func (im *SpecialsImporter) Import() error {
	var err error
	var sheetValues []*SpecialRecord
	sheetValues, err = im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)))

	newOrders := 0
	errOrders := 0
	for i, row := range sheetValues {
		if err := im.createSpecial(row); err != nil {
			slog.Error("importer.createOrderAndPayments", slog.Int("line", i+1), slog.Any("err", err))
			errOrders++
			continue
		}
		newOrders++
	}
	slog.Info("import summary", slog.Int("new_orders", newOrders), slog.Int("with_errors", errOrders))

	return nil
}

type SpecialRecord struct {
	Email       string
	StartDate   time.Time
	EndDate     time.Time
	Category    string
	SubCategory string
}

func (im *SpecialsImporter) getSheetValues() ([]*SpecialRecord, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("sheets.NewService: %w", err)
	}

	call := sheetsService.Spreadsheets.Values.Get(common.SpreadsheetId, common.SpreadsheetRange)
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("sheetsService.Spreadsheets.Values.Get: %w", err)
	}

	records := make([]*SpecialRecord, 0)
	layout := "2006-01-02"

	for _, row := range resp.Values[1:] {
		startDate, err := time.Parse(layout, row[1].(string))
		if err != nil {
			return nil, fmt.Errorf("time.Parse: %w", err)
		}
		endDate, err := time.Parse(layout, row[2].(string))
		if err != nil {
			return nil, fmt.Errorf("time.Parse: %w", err)
		}
		record := &SpecialRecord{
			Email:       row[0].(string),
			StartDate:   startDate,
			EndDate:     endDate,
			Category:    row[3].(string),
			SubCategory: row[4].(string),
		}
		records = append(records, record)
	}
	return records, nil
}

func (im *SpecialsImporter) createSpecial(rSpecial *SpecialRecord) error {

	var special repo.Special
	special.Email = null.StringFrom(rSpecial.Email)
	special.StartDate = null.TimeFrom(rSpecial.StartDate)
	special.EndDate = null.TimeFrom(rSpecial.EndDate)
	special.Category = null.StringFrom(rSpecial.Category)
	special.SubCategory = null.StringFrom(rSpecial.SubCategory)

	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, im)
	account, err := im.repo.GetAccount(ctx, 0, rSpecial.Email)
	if err == nil {
		special.KeycloakId = account.UserKey
	}

	_, err = im.repo.CreateSpecial(ctx, special)
	if err != nil {
		return fmt.Errorf("importer.createSpecial: %w", err)
	}
	return nil
}

func (im *SpecialsImporter) BuildEvent(eventType string, payload map[string]interface{}) events.Event {
	event := events.MakeEvent(eventType, payload)
	event.Component = events.ComponentGenericImporter
	return event
}
