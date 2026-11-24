package importers

import (
	"context"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

// specialKeys indexes the specials already in the table by what a sheet row
// says about one, so a rerun skips rows it already imported. `specials` has no
// unique key; a unique index is the durable fix and needs its own migration.
type specialKeys map[string]bool

// specialZone is the zone sheet dates and API-written specials mean: the API
// stores a local midnight, the importer a UTC one, and both are the same
// Jerusalem calendar day. The prod image has no zoneinfo, hence time/tzdata.
var specialZone = mustLoadLocation("Asia/Jerusalem")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("time.LoadLocation(%q): %v", name, err))
	}
	return loc
}

// anyEnd stands in for the end date of a stored special that was revoked or
// has none, so the sheet row it came from still matches it.
const anyEnd = "\x01any"

// specialKey is identifier + start + end + category + sub-category.
//
// Both identifiers are indexed for a stored row, because createSpecial may add
// a keycloak id the sheet never had. The end date is part of the key so a row
// that moves it is imported, as it was before dedup existed. NULL and empty
// sub-categories are kept apart.
func specialKey(identifier string, start time.Time, end string, category string, subCategory null.String) string {
	sub := "\x01null"
	if subCategory.Valid {
		sub = strings.ToLower(subCategory.String)
	}
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(identifier)),
		specialDay(start),
		end,
		strings.ToLower(category),
		sub,
	}, "\x00")
}

func specialDay(t time.Time) string {
	return t.In(specialZone).Format(time.DateOnly)
}

// storedEnd is a stored special's end for the key. Revoking writes now(), and
// the sheet and the API only ever write a midnight, so an end that is not a
// midnight in either zone is a revoke: it matches whatever end the sheet says,
// or the next run would re-grant it.
func storedEnd(end null.Time) string {
	if !end.Valid || !(isMidnight(end.Time.UTC()) || isMidnight(end.Time.In(specialZone))) {
		return anyEnd
	}
	return specialDay(end.Time)
}

func isMidnight(t time.Time) bool {
	return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
}

func (k specialKeys) addExisting(s *repo.Special) {
	for _, identifier := range []string{s.Email.String, s.KeycloakId.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		k[specialKey(identifier, s.StartDate.Time, storedEnd(s.EndDate), s.Category.String, s.SubCategory)] = true
	}
}

func (k specialKeys) addImported(r *SpecialRecord) {
	for _, identifier := range []string{r.Email.String, r.KeycloakID.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		k[specialKey(identifier, r.StartDate, specialDay(r.EndDate), r.Category, r.SubCategory)] = true
	}
}

// has reports whether either identifier on the record names a stored special.
func (k specialKeys) has(r *SpecialRecord) bool {
	for _, identifier := range []string{r.Email.String, r.KeycloakID.String} {
		if strings.TrimSpace(identifier) == "" {
			continue
		}
		for _, end := range []string{specialDay(r.EndDate), anyEnd} {
			if k[specialKey(identifier, r.StartDate, end, r.Category, r.SubCategory)] {
				return true
			}
		}
	}
	return false
}

// existingSpecials indexes the specials starting within the sheet's date
// range; the table only grows, so reading all of it is unbounded.
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

	// A day either side: the key compares Jerusalem dates, the bounds are instants.
	existing, err := im.repo.GetSpecialsStartingBetween(ctx, from.AddDate(0, 0, -1), to.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("repo.GetSpecialsStartingBetween: %w", err)
	}
	for _, special := range existing {
		keys.addExisting(special)
	}
	return keys, nil
}
