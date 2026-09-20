package importers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
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

// Import reads the sheet and creates a special per row, skipping rows that are
// already in the table.
//
// The importer is not idempotent on its own — CreateSpecial always INSERTs and
// `specials` has no unique key — and the sheet is not cleared between runs. The
// skip is what makes the fix-the-sheet-and-rerun loop safe now that a bad row
// no longer aborts the whole import before anything is written. See
// special_dedup.go; a unique index remains the durable answer and needs its own
// migration.
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

	newRecords := 0
	errRecords := 0
	skippedRecords := 0
	for _, row := range sheetValues {
		if existing.has(row) {
			skippedRecords++
			continue
		}
		// row.SheetRow, not the loop index: sheetValues is the *filtered*
		// slice, so with two rows dropped ahead of it the third survivor is
		// index 0 and sheet row 4. Logging the index sends whoever is fixing
		// the sheet to the wrong line — the off-by-one the parser's sheetRow
		// was introduced to remove, reappearing one function further on.
		if err := im.createSpecial(row); err != nil {
			slog.Error("importer.createSpecial", slog.Int("row", row.SheetRow), slog.Any("err", err))
			errRecords++
			continue
		}
		// Recorded as it goes, so a sheet that lists the same person twice
		// imports them once.
		existing.addImported(row)
		newRecords++
	}
	// dropped is counted separately from with_errors: a row the parser threw
	// away never reaches createSpecial, so without this line it is in no total
	// and the summary adds up to fewer rows than the sheet holds.
	slog.Info("import summary", slog.Int("new_specials", newRecords), slog.Int("already_present", skippedRecords),
		slog.Int("with_errors", errRecords), slog.Int("dropped_by_parser", dropped))

	return nil
}

type SpecialRecord struct {
	Email       null.String
	KeycloakID  null.String
	StartDate   time.Time
	EndDate     time.Time
	Category    string
	SubCategory null.String
	// SheetRow is the 1-based row in the spreadsheet this record came from, so
	// a failure at insert time can name the line an operator has to edit.
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

// parseSpecialRows turns sheet rows into records, skipping the header. Split
// out of getSheetValues, which builds its Sheets client inline and so cannot be
// reached from a test.
//
// Returns the records it could read and the number of data rows it dropped. A
// dropped row is never fatal: reportDroppedRows raises its level when nothing
// survived, which says the same thing without killing a cron that will be
// handed the same sheet a minute later.
func parseSpecialRows(values [][]any) ([]*SpecialRecord, int) {
	records := make([]*SpecialRecord, 0)
	const layout = "2006-01-02"

	// An empty sheet has no header to skip, and values[1:] panics on it rather
	// than reporting an empty import.
	if len(values) == 0 {
		return records, 0
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
			StartDate: startDate,
			EndDate:   endDate,
			Category:  category,
			SheetRow:  sheetRow,
		}

		// Set only when the cell holds something. null.StringFrom("") is Valid
		// and Set, so filling these unconditionally makes createSpecial's
		// "KeycloakID and Email can't both be empty" guard dead code — it asks
		// IsValid, which is true for the empty string. A row with neither
		// identifier then reached GetAccount(ctx, 0, ""), which matches the
		// most recent account carrying an empty email and stamps that person's
		// UserKey on the special.
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

		// Always set, never conditioned on len(row). The Sheets API omits
		// trailing empty cells, so index 5 exists only when some *later* column
		// happens to be filled: the same blank sub-category arrives as a
		// 5-cell row until an operator types a note into column G, and as a
		// 7-cell row afterwards. Keying NULL and the empty string apart then
		// made the dedup depend on a column unrelated to the grant, so an added
		// comment produced a second special for the same person and window.
		//
		// The empty string rather than unset, because the cleanup queries
		// compare the column (`where subcategory <> 'rav'`) and NULL is not the
		// same answer there.
		record.SubCategory = null.StringFrom(cell(row, 5))
		records = append(records, record)
	}

	// Skipping a bad row keeps one broken cell from stopping the whole import.
	// A sheet where *every* row is bad is a different event: one changed date
	// format or one inserted column drops all of them, and the run then logs
	// count=0 with_errors=0 and exits 0 — indistinguishable from an empty
	// sheet, and silent in Sentry. Say so instead.
	return records, dropped
}

func (im *SpecialsImporter) createSpecial(rSpecial *SpecialRecord) error {

	if !rSpecial.KeycloakID.IsValid() && !rSpecial.Email.IsValid() {
		return fmt.Errorf("email & : KeycloakID can't be empty both")
	}
	var special repo.Special
	// An identifier the sheet does not carry is left unset, so the column
	// inserts NULL rather than an empty string. Every reader of specials.email
	// scans null.String, so NULL costs nothing — and an empty one is not inert:
	// DeleteAccount and MergeAccounts delete specials by
	// `email = (SELECT "Email" FROM accounts WHERE id = $1)`, so an account
	// whose own email is empty would match every keycloak-only grant in the
	// table. That query is guarded now, but writing NULL is what keeps this row
	// out of reach of the next such predicate.
	special.Email = rSpecial.Email
	special.KeycloakId = rSpecial.KeycloakID
	special.StartDate = null.TimeFrom(rSpecial.StartDate)
	special.EndDate = null.TimeFrom(rSpecial.EndDate)
	special.Category = null.StringFrom(rSpecial.Category)
	special.SubCategory = rSpecial.SubCategory

	ctx := context.WithValue(context.Background(), common.CtxEventBuilder, im)
	// Resolve the keycloak id from the account, when the sheet gave an email.
	//
	// Guarded on Email.Valid: GetAccount with an empty email matches on
	// LOWER("Email") against an empty literal, ordered by created_at desc limit
	// 1, so it returns whatever account was last created without an email —
	// whose UserKey would then be stamped on this special, granting it to an
	// unrelated person. A row can legitimately carry a keycloak id and no
	// email, so reaching here with an invalid Email is normal.
	//
	// The account's own UserKey has to be a real identifier: nullable, and
	// stored empty on some rows. Either one overwrites a good id from the sheet
	// with something DeleteSpecialsByKeycloakId (keycloak_id = $1) can never
	// match.
	//
	// pgx.ErrNoRows means no such account and is expected; anything else is a
	// real failure and has to surface. The dedup index makes a swallowed error
	// permanent — the row it half-wrote is recognised as already present on
	// every later run, so it is never retried and the special never grants.
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
