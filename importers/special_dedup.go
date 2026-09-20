package importers

import (
	"context"
	"fmt"
	"strings"
	"time"

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
// row carries, plus the window and the categories.
//
// Both identifiers are indexed for an existing row, because they do not survive
// the import symmetrically — createSpecial replaces the keycloak id with the
// matched account's UserKey, so a row imported from an email-only sheet line
// comes back carrying a keycloak id the sheet never had. Matching on the
// identifier the sheet actually provides is what makes the second run
// recognise the first run's work.
func specialKey(identifier string, start, end time.Time, category, subCategory string) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(identifier)),
		// The date, not the instant: these are stored as timestamptz and read
		// back in the session's zone, and the sheet only ever says a date.
		start.UTC().Format(time.DateOnly),
		end.UTC().Format(time.DateOnly),
		strings.ToLower(category),
		strings.ToLower(subCategory),
	}, "\x00")
}

func (k specialKeys) addExisting(s *repo.Special) {
	for _, identifier := range []string{s.Email.String, s.KeycloakId.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		k[specialKey(identifier, s.StartDate.Time, s.EndDate.Time, s.Category.String, s.SubCategory.String)] = true
	}
}

func (k specialKeys) addImported(r *SpecialRecord) {
	for _, identifier := range []string{r.Email.String, r.KeycloakID.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		k[specialKey(identifier, r.StartDate, r.EndDate, r.Category, r.SubCategory.String)] = true
	}
}

// has reports whether either identifier on the record names a special that is
// already there.
func (k specialKeys) has(r *SpecialRecord) bool {
	for _, identifier := range []string{r.Email.String, r.KeycloakID.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		if k[specialKey(identifier, r.StartDate, r.EndDate, r.Category, r.SubCategory.String)] {
			return true
		}
	}
	return false
}

func (im *SpecialsImporter) existingSpecials(ctx context.Context) (specialKeys, error) {
	existing, err := im.repo.GetAllSpecials(ctx)
	if err != nil {
		return nil, fmt.Errorf("repo.GetAllSpecials: %w", err)
	}
	keys := make(specialKeys, len(existing))
	for _, s := range existing {
		keys.addExisting(s)
	}
	return keys, nil
}
