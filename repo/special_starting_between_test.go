package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"
)

func TestGetSpecialsStartingBetween_ExcludesRowsOutsideTheWindow(t *testing.T) {
	db, ctx := newTestDB(t)

	start := time.Now()
	_, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-inside"),
		Email:      null.StringFrom("inside@example.com"),
		StartDate:  null.TimeFrom(start),
		EndDate:    null.TimeFrom(start.Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)

	_, err = db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-outside"),
		Email:      null.StringFrom("outside@example.com"),
		StartDate:  null.TimeFrom(start.AddDate(0, 0, -30)),
		EndDate:    null.TimeFrom(start.Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)

	found, err := db.GetSpecialsStartingBetween(ctx, start.Add(-time.Hour), start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, found, 1, "the 30-day-old row starts outside the window")
	assert.Equal(t, "kc-inside", found[0].KeycloakId.String)
}
