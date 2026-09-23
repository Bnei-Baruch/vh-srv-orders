package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/events"
	"gitlab.bbdev.team/vh/pay/orders/events/eventstest"
	"gitlab.bbdev.team/vh/pay/orders/pkg/testutil"
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

// captureEmitter records what the repo emits instead of publishing it.
type captureEmitter struct{ emitted []events.Event }

func (c *captureEmitter) Emit(_ context.Context, evs ...events.Event) {
	c.emitted = append(c.emitted, evs...)
}

func (c *captureEmitter) Close(context.Context) {}

func newTestDBCapturingEvents(t *testing.T) (*OrdersDB, context.Context, *captureEmitter) {
	t.Helper()
	dbURL, err := testutil.NewTestOrdersDB(t, context.Background())
	require.NoError(t, err)
	emitter := &captureEmitter{}
	db, err := NewOrdersDBUrl(context.Background(), dbURL, emitter)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, eventstest.WithTestEventBuilder(t, context.Background()), emitter
}

// delete_special is the only signal a downstream consumer gets that a grant
// ended, and DeleteSpecialById is the single primitive that emits it —
// DeleteSpecialsByKeycloakId funnels through it. A keycloak-only special stores
// an empty email, so an email-only payload names nobody the consumer can act
// on; against an ilike predicate it names everyone with an empty email.
func TestDeleteSpecialById_EventCarriesTheKeycloakId(t *testing.T) {
	db, ctx, emitter := newTestDBCapturingEvents(t)

	id, err := db.CreateSpecial(ctx, Special{
		KeycloakId: null.StringFrom("kc-revoked"),
		Email:      null.StringFrom(""),
		StartDate:  null.TimeFrom(time.Now().Add(-time.Hour)),
		EndDate:    null.TimeFrom(time.Now().Add(24 * time.Hour)),
		Category:   null.StringFrom("membership"),
	})
	require.NoError(t, err)

	require.NoError(t, db.DeleteSpecialById(ctx, id))

	var payload map[string]interface{}
	for _, e := range emitter.emitted {
		if e.Type == events.TypeDeleteSpecial {
			payload = e.Payload
		}
	}
	require.NotNil(t, payload, "revoking must emit delete_special")
	assert.Equal(t, "kc-revoked", payload["keycloak_id"], "the revoke must name who was revoked")
}

// The listing is bounded because the table only grows — revoking rewrites
// end_date rather than removing the row — and ordered, because LIMIT/OFFSET
// without an ORDER BY lets two pages overlap or skip rows.
//
// created_at is scrambled after insert on purpose. Left in insertion order the
// assertion passes with or without the ORDER BY, because that is the order
// Postgres happens to return a small table in — which is exactly how an
// ordering nobody pinned survives into production.
func TestGetAllSpecials_IsPagedAndOrdered(t *testing.T) {
	db, ctx := newTestDB(t)

	start := time.Now()
	ids := make([]int, 3)
	for i := range ids {
		id, err := db.CreateSpecial(ctx, Special{
			KeycloakId: null.StringFrom(fmt.Sprintf("kc-%d", i)),
			Email:      null.StringFrom(fmt.Sprintf("p%d@example.com", i)),
			StartDate:  null.TimeFrom(start),
			EndDate:    null.TimeFrom(start.Add(24 * time.Hour)),
			Category:   null.StringFrom("membership"),
		})
		require.NoError(t, err)
		ids[i] = id
		t.Cleanup(func() { _ = db.DeleteSpecialById(ctx, id) })
	}

	// Newest first is ids[1], then ids[0], then ids[2] — deliberately not the
	// order the rows were written in.
	for offset, id := range map[int]int{0: ids[2], 1: ids[0], 2: ids[1]} {
		_, err := db.Exec(ctx, `UPDATE specials SET created_at = $1 WHERE id = $2`,
			start.Add(time.Duration(offset)*time.Minute), id)
		require.NoError(t, err)
	}
	want := []int{ids[1], ids[0], ids[2]}

	first, err := db.GetAllSpecials(ctx, 0, 2)
	require.NoError(t, err)
	require.Len(t, first, 2, "limit must bound the read")
	assert.Equal(t, want[:2], []int{first[0].Id.Int, first[1].Id.Int}, "newest first")

	second, err := db.GetAllSpecials(ctx, 2, 2)
	require.NoError(t, err)
	require.Len(t, second, 1, "offset must skip the first page")
	assert.Equal(t, want[2], second[0].Id.Int, "and the pages must not overlap")
}
