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

	newRecords := 0
	errRecords := 0
	for i, row := range sheetValues {
		if err := im.createSpecial(row); err != nil {
			slog.Error("importer.createSpecial", slog.Int("line", i+1), slog.Any("err", err))
			errRecords++
			continue
		}
		newRecords++
	}
	slog.Info("import summary", slog.Int("new_specials", newRecords), slog.Int("with_errors", errRecords))

	return nil
}

type SpecialRecord struct {
	Email       null.String
	KeycloakID  null.String
	StartDate   time.Time
	EndDate     time.Time
	Category    string
	SubCategory null.String
}

func (im *SpecialsImporter) getSheetValues() ([]*SpecialRecord, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("sheets.NewService: %w", err)
	}

	call := sheetsService.Spreadsheets.Values.Get(common.Config.ImportSpecialsSpreadsheetId, "import specials")
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("sheetsService.Spreadsheets.Values.Get: %w", err)
	}

	return parseSpecialRows(resp.Values)
}

// parseSpecialRows turns sheet rows into records, skipping the header. Split
// out of getSheetValues, which builds its Sheets client inline and so cannot be
// reached from a test.
func parseSpecialRows(values [][]any) ([]*SpecialRecord, error) {
	records := make([]*SpecialRecord, 0)
	const layout = "2006-01-02"

	// An empty sheet has no header to skip, and values[1:] panics on it rather
	// than reporting an empty import.
	if len(values) == 0 {
		return records, nil
	}

	for i, row := range values[1:] {
		// +2: i counts from the first data row, and the header is sheet row 1.
		sheetRow := i + 2

		startDate, err := time.Parse(layout, cell(row, 2))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "start_date"), slog.Any("err", err))
			continue
		}
		endDate, err := time.Parse(layout, cell(row, 3))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "end_date"), slog.Any("err", err))
			continue
		}

		record := &SpecialRecord{
			Email:      null.StringFrom(cell(row, 0)),
			KeycloakID: null.StringFrom(cell(row, 1)),
			StartDate:  startDate,
			EndDate:    endDate,
			Category:   cell(row, 4),
		}
		if sub := cell(row, 5); sub != "" {
			record.SubCategory = null.StringFrom(sub)
		}
		records = append(records, record)
	}
	return records, nil
}

func (im *SpecialsImporter) createSpecial(rSpecial *SpecialRecord) error {

	if !rSpecial.KeycloakID.IsValid() && !rSpecial.Email.IsValid() {
		return fmt.Errorf("email & : KeycloakID can't be empty both")
	}
	var special repo.Special
	special.Email = rSpecial.Email
	special.KeycloakId = rSpecial.KeycloakID
	special.StartDate = null.TimeFrom(rSpecial.StartDate)
	special.EndDate = null.TimeFrom(rSpecial.EndDate)
	special.Category = null.StringFrom(rSpecial.Category)
	special.SubCategory = rSpecial.SubCategory

	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, im)
	account, err := im.repo.GetAccount(ctx, 0, rSpecial.Email.String)
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
	event.Component = events.ComponentSpecialImporter
	event.Actor = events.ActorSystem
	return event
}
