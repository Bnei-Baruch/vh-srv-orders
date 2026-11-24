package importers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/volatiletech/null/v9"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
	"github.com/Bnei-Baruch/vh-srv-orders/events"
	"github.com/Bnei-Baruch/vh-srv-orders/repo"
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

// Import creates a special per sheet row, skipping rows already in the table
// (see special_dedup.go): the sheet is never cleared and CreateSpecial always
// INSERTs.
func (im *SpecialsImporter) Import() error {
	sheetValues, dropped, err := im.getSheetValues()
	if err != nil {
		return fmt.Errorf("importer.getSheetValues: %w", err)
	}
	slog.Info("importer.getSheetValues", slog.Int("count", len(sheetValues)), slog.Int("dropped", dropped))
	reportDroppedRows(im, dropped, len(sheetValues))

	existing, err := im.existingSpecials(context.WithValue(context.Background(), common.CtxEventBuilder, im), sheetValues)
	if err != nil {
		return fmt.Errorf("importer.existingSpecials: %w", err)
	}

	newRecords, skippedRecords, errRecords := importSpecialRecords(sheetValues, existing, im.createSpecial)
	slog.Info("import summary", slog.Int("new_specials", newRecords), slog.Int("already_present", skippedRecords),
		slog.Int("with_errors", errRecords), slog.Int("dropped_by_parser", dropped))

	return nil
}

// importSpecialRecords creates each record the table does not already hold.
func importSpecialRecords(records []*SpecialRecord, existing specialKeys, create func(*SpecialRecord) error) (created, skipped, failed int) {
	for _, row := range records {
		if existing.has(row) {
			skipped++
			continue
		}
		if err := create(row); err != nil {
			slog.Error("importer.createSpecial", slog.Int("row", row.SheetRow), slog.Any("err", err))
			failed++
			continue
		}
		// so a sheet that lists the same grant twice imports it once
		existing.addImported(row)
		created++
	}
	return created, skipped, failed
}

type SpecialRecord struct {
	Email       null.String
	KeycloakID  null.String
	StartDate   time.Time
	EndDate     time.Time
	Category    string
	SubCategory null.String
	// SheetRow is the 1-based sheet row, for log lines an operator can act on.
	SheetRow int
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

	records, dropped := parseSpecialRows(resp.Values)
	return records, dropped, nil
}

// parseSpecialRows turns sheet rows into records, skipping the header. It
// returns the records it could read and how many data rows it dropped; a bad
// row is logged and skipped, never fatal.
func parseSpecialRows(values [][]any) ([]*SpecialRecord, int) {
	records := make([]*SpecialRecord, 0)
	const layout = "2006-01-02"
	if len(values) == 0 {
		return records, 0
	}

	dropped := 0
	for i, row := range values[1:] {
		sheetRow := i + 2 // the header is row 1
		if blankRow(row) {
			continue
		}

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

		// An empty category inserts '' and silently grants nothing.
		category := cell(row, 4)
		if category == "" {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "category"), slog.String("reason", "empty"))
			dropped++
			continue
		}

		record := &SpecialRecord{
			StartDate: startDate,
			EndDate:   endDate,
			Category:  category,
			SheetRow:  sheetRow,
		}

		// Set only when non-empty: null.StringFrom("") is Valid, which would
		// defeat createSpecial's identifier checks.
		if email := cell(row, 0); email != "" {
			record.Email = null.StringFrom(email)
		}
		if keycloakID := cell(row, 1); keycloakID != "" {
			record.KeycloakID = null.StringFrom(keycloakID)
		}
		if !record.Email.Valid && !record.KeycloakID.Valid {
			slog.Warn("malformed row", slog.Int("row", sheetRow), slog.String("column", "email/keycloak_id"), slog.String("reason", "both empty"))
			dropped++
			continue
		}

		// Always '', never NULL and never decided by len(row): trailing cells
		// are omitted, so the row length depends on unrelated later columns,
		// and the cleanup queries (`subcategory <> 'rav'`) treat NULL differently.
		record.SubCategory = null.StringFrom(cell(row, 5))
		records = append(records, record)
	}
	return records, dropped
}

func (im *SpecialsImporter) createSpecial(rSpecial *SpecialRecord) error {

	// A special without an email could never be revoked: vh-srv-profile
	// revokes by email. CreateSpecial refuses it too; this names the cause.
	if strings.TrimSpace(rSpecial.Email.String) == "" {
		return fmt.Errorf("email is required (keycloak id %q)", rSpecial.KeycloakID.String)
	}
	var special repo.Special
	// An identifier the sheet does not carry stays unset, so the column is
	// omitted from the INSERT and stores NULL, never ''.
	special.Email = rSpecial.Email
	special.KeycloakId = rSpecial.KeycloakID
	special.StartDate = null.TimeFrom(rSpecial.StartDate)
	special.EndDate = null.TimeFrom(rSpecial.EndDate)
	special.Category = null.StringFrom(rSpecial.Category)
	special.SubCategory = rSpecial.SubCategory

	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, im)
	// The account may add a keycloak id, never replace a good one with an empty
	// one. Only pgx.ErrNoRows is expected: a swallowed error would leave a
	// half-written row that the dedup then skips forever.
	if rSpecial.Email.Valid {
		account, err := im.repo.GetAccount(ctx, 0, rSpecial.Email.String)
		switch {
		case err == nil:
			if account.UserKey.String != "" {
				special.KeycloakId = account.UserKey
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("repo.GetAccount: %w", err)
		}
	}

	if _, err := im.repo.CreateSpecial(ctx, special); err != nil {
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
