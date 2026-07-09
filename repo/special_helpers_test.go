package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertSpecial creates a specials row with the given activity window.
func insertSpecial(t *testing.T, db *OrdersDB, ctx context.Context, keycloakID, email string, startOffset, endOffset time.Duration) int {
	t.Helper()
	var id int
	err := db.QueryRow(ctx,
		`INSERT INTO specials (keycloak_id, email, start_date, end_date, category)
		 VALUES ($1, $2, $3, $4, 'test') RETURNING id`,
		keycloakID, email, time.Now().Add(startOffset), time.Now().Add(endOffset),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func specialEndDate(t *testing.T, db *OrdersDB, ctx context.Context, id int) time.Time {
	t.Helper()
	var end time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT end_date FROM specials WHERE id=$1`, id).Scan(&end))
	return end
}

// Revoking by keycloak_id ends only the currently-active span: past spans keep their
// original end date (history is not rewritten) and future spans stay scheduled.
func TestDeleteSpecialsByKeycloakId_EndsOnlyActiveSpan(t *testing.T) {
	db, ctx := newTestDB(t)

	past := insertSpecial(t, db, ctx, "kc-spec", "spec@test.test", -72*time.Hour, -48*time.Hour)
	active := insertSpecial(t, db, ctx, "kc-spec", "spec@test.test", -time.Hour, 24*time.Hour)
	future := insertSpecial(t, db, ctx, "kc-spec", "spec@test.test", 48*time.Hour, 96*time.Hour)
	other := insertSpecial(t, db, ctx, "kc-other", "other@test.test", -time.Hour, 24*time.Hour)

	pastEnd := specialEndDate(t, db, ctx, past)
	futureEnd := specialEndDate(t, db, ctx, future)
	otherEnd := specialEndDate(t, db, ctx, other)

	require.NoError(t, db.DeleteSpecialsByKeycloakId(ctx, "kc-spec"))

	assert.WithinDuration(t, time.Now(), specialEndDate(t, db, ctx, active), 5*time.Second,
		"active span should be ended now")
	assert.Equal(t, pastEnd, specialEndDate(t, db, ctx, past), "past span history should be untouched")
	assert.Equal(t, futureEnd, specialEndDate(t, db, ctx, future), "future span should stay scheduled")
	assert.Equal(t, otherEnd, specialEndDate(t, db, ctx, other), "other user's specials should be untouched")
}

// With several simultaneously-active (overlapping) spans, revoking by keycloak_id must
// end all of them — the eval picks an arbitrary active span, so leaving one live would
// keep the user active despite the revoke.
func TestDeleteSpecialsByKeycloakId_MultipleActiveSpans_AllEnded(t *testing.T) {
	db, ctx := newTestDB(t)

	first := insertSpecial(t, db, ctx, "kc-multi", "multi@test.test", -48*time.Hour, 24*time.Hour)
	second := insertSpecial(t, db, ctx, "kc-multi", "multi@test.test", -time.Hour, 96*time.Hour)

	require.NoError(t, db.DeleteSpecialsByKeycloakId(ctx, "kc-multi"))

	assert.WithinDuration(t, time.Now(), specialEndDate(t, db, ctx, first), 5*time.Second)
	assert.WithinDuration(t, time.Now(), specialEndDate(t, db, ctx, second), 5*time.Second)
}

func TestDeleteSpecialsByKeycloakId_NoActiveSpan_Succeeds(t *testing.T) {
	db, ctx := newTestDB(t)

	past := insertSpecial(t, db, ctx, "kc-spec-none", "none@test.test", -72*time.Hour, -48*time.Hour)
	pastEnd := specialEndDate(t, db, ctx, past)

	require.NoError(t, db.DeleteSpecialsByKeycloakId(ctx, "kc-spec-none"))

	assert.Equal(t, pastEnd, specialEndDate(t, db, ctx, past))
}
