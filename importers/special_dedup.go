package importers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// specialKeys indexes the specials already in the table by what a sheet row
// says about one, so a row that has already been imported can be recognised.
//
// The importer needs this because it is not idempotent and the sheet is not
// cleared between runs. CreateSpecial always INSERTs and `specials` carries no
// unique key, so before per-row skipping the abort on the first bad date was
// what made a rerun safe: nothing had been written yet. Now a run that drops 3
// rows of 200 has already committed the other 197, and rerunning after fixing
// those 3 would give 197 people a second row each. The duplicates are not
// inert — GetAllSpecialsByEmail feeds specialActivator, and
// DeleteSpecialsByKeycloakId has to end each active row individually, so a
// partially duplicated table changes what revoking means.
//
// A unique index is the durable answer and needs its own migration plus a
// decision about what to do with the duplicates already in the table. This is
// the same shape the Robokasa importer already uses: read what exists, skip
// what matches.
type specialKeys map[string]bool

// key identifies a special the way the sheet does: by whichever identifier the
// row carries, plus the start of the window and the categories.
//
// Both identifiers are indexed for an existing row, because they do not survive
// the import symmetrically — createSpecial replaces the keycloak id with the
// matched account's UserKey, so a row imported from an email-only sheet line
// comes back carrying a keycloak id the sheet never had. Matching on the
// identifier the sheet actually provides is what makes the second run
// recognise the first run's work.
//
// end_date is deliberately not part of the key. Revoking is a soft update that
// rewrites exactly that column (DeleteSpecialById: `SET end_date = now()`), so
// a key that included it stopped matching the sheet row the moment an admin
// revoked the grant, and the next cron run silently re-granted it. The cost is
// that editing only the end date in the sheet no longer creates a second,
// longer row — an extension has to go through the API.
func specialKey(identifier string, start time.Time, category string, subCategory null.String) string {
	// NULL and an empty sub-category are different rows: parseSpecialRows keeps
	// them apart because the cleanup queries compare the column, and
	// `where subcategory <> 'rav'` does not answer the same for both.
	sub := "\x01null"
	if subCategory.Valid {
		sub = strings.ToLower(subCategory.String)
	}
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(identifier)),
		// The date, not the instant: these are stored as timestamptz and read
		// back in the session's zone, and the sheet only ever says a date.
		start.UTC().Format(time.DateOnly),
		strings.ToLower(category),
		sub,
	}, "\x00")
}

func (k specialKeys) addExisting(s *repo.Special) {
	for _, identifier := range []string{s.Email.String, s.KeycloakId.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		k[specialKey(identifier, s.StartDate.Time, s.Category.String, s.SubCategory)] = true
	}
}

func (k specialKeys) addImported(r *SpecialRecord) {
	for _, identifier := range []string{r.Email.String, r.KeycloakID.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		k[specialKey(identifier, r.StartDate, r.Category, r.SubCategory)] = true
	}
}

// has reports whether either identifier on the record names a special that is
// already there.
func (k specialKeys) has(r *SpecialRecord) bool {
	for _, identifier := range []string{r.Email.String, r.KeycloakID.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		if k[specialKey(identifier, r.StartDate, r.Category, r.SubCategory)] {
			return true
		}
	}
	return false
}

// existingSpecials indexes only the specials whose start date the sheet could
// name. The table is append-only in practice — revoking moves end_date rather
// than deleting — so reading all of it grows without bound, and a row whose
// start is outside the sheet's range can never match a key that contains it.
func (im *SpecialsImporter) existingSpecials(ctx context.Context, records []*SpecialRecord) (specialKeys, error) {
	keys := make(specialKeys)
	if len(records) == 0 {
		return keys, nil
	}

	from, to := records[0].StartDate, records[0].StartDate
	for _, record := range records {
		if record.StartDate.Before(from) {
			from = record.StartDate
		}
		if record.StartDate.After(to) {
			to = record.StartDate
		}
	}

	// A day either side: the column is timestamptz and the key compares UTC
	// dates, so a row stored at a local midnight sits on the other side of an
	// exact bound.
	existing, err := im.repo.GetSpecialsStartingBetween(ctx, from.AddDate(0, 0, -1), to.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("repo.GetSpecialsStartingBetween: %w", err)
	}
	for _, special := range existing {
		keys.addExisting(special)
	}
	return keys, nil
}
