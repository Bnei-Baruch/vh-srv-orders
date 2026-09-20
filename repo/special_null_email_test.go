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
// can be created through the API. DeleteSpecialById scanned that column into a
// plain string, where pgx refuses NULL:
//
//	can't scan into dest[0] (col: email): cannot scan NULL into *string
//
// That is the revoke path, so such a row could not be removed at all.
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

	found, err := db.GetSpecialsStartingBetween(ctx, time.Now().Add(-24*time.Hour), time.Now().Add(24*time.Hour))
	require.NoError(t, err, "a NULL-email row must not stop the activator reading the rest")
	assert.Len(t, found, 2)

	require.NoError(t, db.DeleteSpecialById(ctx, nullEmail), "a NULL-email special must still be revocable")
	require.NoError(t, db.DeleteSpecialById(ctx, withEmail))
}

// A keycloak-only special is stored with an empty email, and two of them are
// two people. The activator used to reach specials through the distinct emails,
// so every keycloak-only row shared the key "" and collapsed into one.
func TestSpecials_KeycloakOnlySpecialsAreDistinctRows(t *testing.T) {
	db, ctx := newTestDB(t)

	start := time.Now()
	for _, keycloakID := range []string{"kc-alice", "kc-bob"} {
		id, err := db.CreateSpecial(ctx, Special{
			KeycloakId: null.StringFrom(keycloakID),
			Email:      null.StringFrom(""),
			StartDate:  null.TimeFrom(start),
			EndDate:    null.TimeFrom(start.Add(24 * time.Hour)),
			Category:   null.StringFrom("membership"),
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.DeleteSpecialById(ctx, id) })
	}

	found, err := db.GetSpecialsStartingBetween(ctx, start.Add(-time.Hour), start.Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, found, 2, "both rows must come back; the empty email is not an identity")

	keys := []string{found[0].KeycloakId.String, found[1].KeycloakId.String}
	assert.ElementsMatch(t, []string{"kc-alice", "kc-bob"}, keys)
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

// HardDeleteAllUserDataByAccountID and MergeAccountsOrders remove an account's
// specials with
// `email = (SELECT "Email" FROM accounts WHERE id = $1)`. Accounts whose own
// email is the empty string exist — the guard in createSpecial's account lookup
// is there because GetAccount(ctx, 0, "") resolves to the most recent one — and
// without NULLIF that predicate matches every special stored with an empty
// email, i.e. every keycloak-only grant in the table, for every user.
func TestHardDeleteAllUserData_EmptyEmailDoesNotTakeEveryKeycloakOnlySpecial(t *testing.T) {
	db, ctx := newTestDB(t)

	emptyEmailAccount, err := db.CreateAccount(ctx, Account{
		Email:   null.StringFrom(""),
		UserKey: null.StringFrom("kc-owner"),
	})
	require.NoError(t, err)

	// Somebody else's keycloak-only special, stored the way the importer used
	// to write them.
	bystander, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-bystander"),
		Email:      null.StringFrom(""),
		StartDate:  null.TimeFrom(time.Now().Add(-time.Hour)),
		EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteSpecialById(ctx, bystander) })

	require.NoError(t, db.HardDeleteAllUserDataByAccountID(ctx, emptyEmailAccount, "kc-owner"))

	var survives bool
	require.NoError(t, db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM specials WHERE id = $1)`, bystander).Scan(&survives))
	assert.True(t, survives, "deleting an account with an empty email must not delete other people's specials")
}

// A keycloak-only special stores NULL in email, and `ilike` is UNKNOWN against
// NULL, so the by-email listing does not return it for any pattern including a
// wildcard. That is the intended answer — the grant belongs to no address — and
// it is pinned because the rows used to store an empty string, which a wildcard
// did match. The keycloak path is how such a special is found.
func TestSpecials_KeycloakOnlySpecialIsFoundByIdNotByEmail(t *testing.T) {
	db, ctx := newTestDB(t)

	id, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-no-email"),
		StartDate:  null.TimeFrom(time.Now()),
		EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DeleteSpecialById(ctx, id) })

	var isNull bool
	require.NoError(t, db.QueryRow(ctx, `SELECT email IS NULL FROM specials WHERE id = $1`, id).Scan(&isNull))
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
