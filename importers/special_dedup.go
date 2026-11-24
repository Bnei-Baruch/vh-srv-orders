package importers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/volatiletech/null/v9"

	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

// specialKeys indexes the specials already in the table by what a sheet row
// says about one, so a rerun skips rows it already imported. `specials` has no
// unique key; a unique index is the durable fix and needs its own migration.
type specialKeys map[string]bool

// specialKey is identifier + start date + category + sub-category.
//
// Both identifiers are indexed for a stored row, because createSpecial may add
// a keycloak id the sheet never had. end_date is left out: revoking rewrites
// it, and including it made the next run re-grant anything revoked. The date
// is in the process's zone, since a local midnight east of UTC is the previous
// UTC day. NULL and empty sub-categories are kept apart.
func specialKey(identifier string, start time.Time, category string, subCategory null.String) string {
	sub := "\x01null"
	if subCategory.Valid {
		sub = strings.ToLower(subCategory.String)
	}
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(identifier)),
		start.Local().Format(time.DateOnly),
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

// has reports whether either identifier on the record names a stored special.
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

	// A day either side: the key compares local dates, the bounds are instants.
	existing, err := im.repo.GetSpecialsStartingBetween(ctx, from.AddDate(0, 0, -1), to.AddDate(0, 0, 1))
	if err != nil {
		return nil, fmt.Errorf("repo.GetSpecialsStartingBetween: %w", err)
	}
	for _, special := range existing {
		keys.addExisting(special)
	}
	return keys, nil
}
