package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// specials.email is nullable, and rows without an email predate #53, which
// made CreateSpecial require one. DeleteSpecialById scanned that column into a
// plain string, where pgx refuses NULL:
//
//	can't scan into dest[0] (col: email): cannot scan NULL into *string
//
// Such a row must not stop the activator reading the rest, and revoking it is
// refused by name rather than failing on the scan: vh-srv-profile revokes by
// email, so ending it here would leave the membership active there.
func TestSpecials_NullEmailRowIsReadableAndRefusedOnRevoke(t *testing.T) {
	db, ctx := newTestDB(t)

	withEmail, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-with-email"),
		Email:      null.StringFrom("has@example.com"),
		StartDate:  null.TimeFrom(time.Now().Add(-time.Hour)),
		EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)

	nullEmail := insertLegacySpecial(t, db, ctx, "kc-no-email", nil)

	found, err := db.GetSpecialsStartingBetween(ctx, time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour))
	require.NoError(t, err, "a NULL-email row must not stop the activator reading the rest")
	assert.Len(t, found, 2)

	require.ErrorIs(t, db.DeleteSpecialById(ctx, nullEmail), common.ErrInvalidValues)
	require.NoError(t, db.DeleteSpecialById(ctx, withEmail))
}

// insertLegacySpecial writes a row the way specials were stored before #53 —
// email NULL or a given string — which CreateSpecial no longer accepts.
func insertLegacySpecial(t *testing.T, db *OrdersDB, ctx context.Context, keycloakID string, email *string) int {
	t.Helper()
	var id int
	require.NoError(t, db.pool.QueryRow(ctx,
		`INSERT INTO specials (keycloak_id, email, start_date, end_date, category)
		 VALUES ($1, $2, now() - interval '1 hour', now() + interval '1 day', 'membership') RETURNING id`,
		keycloakID, email).Scan(&id))
	return id
}

// GetSpecialsStartingBetween is what bounds both the activator's read and the
// importer's dedup index: a row starting outside the window must not come back.
func TestSpecials_StartingBetweenExcludesRowsOutsideTheWindow(t *testing.T) {
	db, ctx := newTestDB(t)

	start := time.Now()
	inside, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-inside"),
		Email:      null.StringFrom("inside@example.com"),
		StartDate:  null.TimeFrom(start),
		EndDate:    null.TimeFrom(start.Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteSpecialById(ctx, inside) })

	outside, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-outside"),
		Email:      null.StringFrom("outside@example.com"),
		StartDate:  null.TimeFrom(start.AddDate(0, 0, -30)),
		EndDate:    null.TimeFrom(start.Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteSpecialById(ctx, outside) })

	found, err := db.GetSpecialsStartingBetween(ctx, start.Add(-time.Hour), start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, found, 1, "the 30-day-old row starts outside the window")
	assert.Equal(t, "kc-inside", found[0].KeycloakId.String)
}

// A keycloak-only special (from before #53) stores NULL in email, and `ilike` is UNKNOWN against
// NULL, so the by-email listing does not return it for any pattern including a
// wildcard. That is the intended answer — the grant belongs to no address — and
// it is pinned because the rows used to store an empty string, which a wildcard
// did match. The keycloak path is how such a special is found.
func TestSpecials_KeycloakOnlySpecialIsFoundByIdNotByEmail(t *testing.T) {
	db, ctx := newTestDB(t)

	id := insertLegacySpecial(t, db, ctx, "kc-no-email", nil)

	var isNull bool
	require.NoError(t, db.pool.QueryRow(ctx, `SELECT email IS NULL FROM specials WHERE id = $1`, id).Scan(&isNull))
	require.True(t, isNull, "the fixture must store NULL, or this test proves nothing")

	for _, pattern := range []string{"", "%", "kc-no-email"} {
		found, err := db.GetAllSpecialsByEmail(ctx, pattern)
		require.NoError(t, err)
		assert.Empty(t, found, "a grant with no address is not found by address %q", pattern)
	}

	byKeycloak, err := db.GetSpecialsByKeycloakId(ctx, "kc-no-email")
	require.NoError(t, err)
	require.Len(t, byKeycloak, 1, "and the keycloak path is how it is reached")
}
