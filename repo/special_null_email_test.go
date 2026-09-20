package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"
)

// specials.email is nullable, and handleCreateSpecial binds repo.Special
// straight from the request with no email required, so rows with a NULL email
// can be created through the API. Both readers below scanned that column into a
// plain string, where pgx refuses NULL:
//
//	can't scan into dest[0] (col: email): cannot scan NULL into *string
//
// GetUniqueEmailsFromSpecial is the first thing specialActivator.DoTask calls,
// so one such row stopped every special from activating, for every user;
// DeleteSpecialById is the revoke path, so the row could not be removed either.
func TestSpecials_NullEmailRowIsReadableAndRevocable(t *testing.T) {
	db, ctx := newTestDB(t)

	withEmail, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-with-email"),
		Email:      null.StringFrom("has@example.com"),
		StartDate:  null.TimeFrom(time.Now().Add(-time.Hour)),
		EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)

	// Email left unset — the shape the API can produce.
	nullEmail, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-no-email"),
		StartDate:  null.TimeFrom(time.Now().Add(-time.Hour)),
		EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)

	var isNull bool
	require.NoError(t, db.QueryRow(ctx, `SELECT email IS NULL FROM specials WHERE id = $1`, nullEmail).Scan(&isNull))
	require.True(t, isNull, "the fixture must actually store NULL, or this test proves nothing")

	emails, err := db.GetUniqueEmailsFromSpecial(ctx)
	require.NoError(t, err, "one NULL-email row must not stop the activator reading the rest")
	assert.Contains(t, emails, "has@example.com")
	assert.NotContains(t, emails, "", "a row with no email contributes no email to look up")

	require.NoError(t, db.DeleteSpecialById(ctx, nullEmail), "a NULL-email special must still be revocable")
	require.NoError(t, db.DeleteSpecialById(ctx, withEmail))
}
