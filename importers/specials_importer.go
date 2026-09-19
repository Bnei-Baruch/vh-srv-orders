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
	sheetValues, dropped, err := im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)), slog.Int("dropped", dropped))

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
	// dropped is counted separately from with_errors: a row the parser threw
	// away never reaches createSpecial, so without this line it is in no total
	// and the summary adds up to fewer rows than the sheet holds.
	slog.Info("import summary", slog.Int("new_specials", newRecords), slog.Int("with_errors", errRecords), slog.Int("dropped_by_parser", dropped))

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

func (im *SpecialsImporter) getSheetValues() ([]*SpecialRecord, int, error) {
	sheetsService, err := sheets.NewService(context.TODO(),
		option.WithCredentialsFile(common.Config.GoogleAppCredentials),
		option.WithScopes(sheets.SpreadsheetsReadonlyScope))
	if err != nil {
		return nil, 0, fmt.Errorf("sheets.NewService: %w", err)
	}

	call := sheetsService.Spreadsheets.Values.Get(common.Config.ImportSpecialsSpreadsheetId, "import specials")
	call.Context(context.TODO())
	resp, err := call.Do()
	if err != nil {
		return nil, 0, fmt.Errorf("sheetsService.Spreadsheets.Values.Get: %w", err)
	}

	return parseSpecialRows(resp.Values)
}

// parseSpecialRows turns sheet rows into records, skipping the header. Split
// out of getSheetValues, which builds its Sheets client inline and so cannot be
// reached from a test.
//
// Returns the records it could read and the number of data rows it dropped.
func parseSpecialRows(values [][]any) ([]*SpecialRecord, int, error) {
	records := make([]*SpecialRecord, 0)
	const layout = "2006-01-02"

	// An empty sheet has no header to skip, and values[1:] panics on it rather
	// than reporting an empty import.
	if len(values) == 0 {
		return records, 0, nil
	}

	dropped := 0
	for i, row := range values[1:] {
		// +2: i counts from the first data row, and the header is sheet row 1.
		sheetRow := i + 2

		startDate, err := time.Parse(layout, cell(row, 2))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "start_date"), slog.Any("err", err))
			dropped++
			continue
		}
		endDate, err := time.Parse(layout, cell(row, 3))
		if err != nil {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "end_date"), slog.Any("err", err))
			dropped++
			continue
		}

		// specials.category is varchar(50) NOT NULL, and null.StringFrom("") is
		// Valid, so a missing category inserts '' rather than being rejected.
		// The row then grants nothing — no category matches — and does it
		// silently. A row that cannot say what it grants is malformed.
		category := cell(row, 4)
		if category == "" {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "category"), slog.String("reason", "empty"))
			dropped++
			continue
		}

		record := &SpecialRecord{
			Email:      null.StringFrom(cell(row, 0)),
			KeycloakID: null.StringFrom(cell(row, 1)),
			StartDate:  startDate,
			EndDate:    endDate,
			Category:   category,
		}
		if sub := cell(row, 5); sub != "" {
			record.SubCategory = null.StringFrom(sub)
		}
		records = append(records, record)
	}

	// Skipping a bad row keeps one broken cell from stopping the whole import.
	// A sheet where *every* row is bad is a different event: one changed date
	// format or one inserted column drops all of them, and the run then logs
	// count=0 with_errors=0 and exits 0 — indistinguishable from an empty
	// sheet, and silent in Sentry. Say so instead.
	if dropped > 0 && len(records) == 0 {
		return nil, dropped, fmt.Errorf("every data row was dropped (%d of %d); the sheet format has probably changed", dropped, len(values)-1)
	}

	return records, dropped, nil
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
