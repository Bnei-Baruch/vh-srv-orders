package repo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// insertHHGrant creates an approved request and its linked grant directly,
// bypassing the conclude flow, with the given activity window.
func insertHHGrant(t *testing.T, db *OrdersDB, ctx context.Context, keycloakID string, startOffset, endOffset time.Duration) int {
	t.Helper()
	var requestID int
	err := db.QueryRow(ctx,
		`INSERT INTO hh_requests (keycloak_id, type, requested_pct, months, status)
		 VALUES ($1, $2, 100, 6, $3) RETURNING id`,
		keycloakID, common.HHGrantTypeOther, common.HHRequestStatusApproved,
	).Scan(&requestID)
	require.NoError(t, err)

	var id int
	err = db.QueryRow(ctx,
		`INSERT INTO hh_grants (request_id, keycloak_id, type, discount_pct, start_date, end_date)
		 VALUES ($1, $2, $3, 100, $4, $5) RETURNING id`,
		requestID, keycloakID, common.HHGrantTypeOther, time.Now().Add(startOffset), time.Now().Add(endOffset),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestCancelHHGrant_EndsActiveGrant(t *testing.T) {
	db, ctx := newTestDB(t)

	insertHHGrant(t, db, ctx, "kc-hh-cancel", -time.Hour, 24*time.Hour)

	active, err := db.GetActiveHHGrant(ctx, "kc-hh-cancel")
	require.NoError(t, err)
	assert.NotNil(t, active, "grant should be active before cancellation")

	err = db.CancelHHGrant(ctx, "kc-hh-cancel")
	require.NoError(t, err)

	active, err = db.GetActiveHHGrant(ctx, "kc-hh-cancel")
	require.NoError(t, err)
	assert.Nil(t, active, "grant should not be active after cancellation")
}

func TestCancelHHGrant_NoActiveGrant_ReturnsErrNoRowsAffected(t *testing.T) {
	db, ctx := newTestDB(t)

	err := db.CancelHHGrant(ctx, "kc-hh-none")
	require.ErrorIs(t, err, common.ErrNoRowsAffected)
}

func TestGetActiveHHGrant_UnknownKeycloakID_ReturnsNil(t *testing.T) {
	db, ctx := newTestDB(t)

	grant, err := db.GetActiveHHGrant(ctx, "kc-hh-unknown")
	require.NoError(t, err)
	assert.Nil(t, grant)
}

func TestGetActiveHHGrant_ExpiredGrant_ReturnsNil(t *testing.T) {
	db, ctx := newTestDB(t)

	insertHHGrant(t, db, ctx, "kc-hh-expired", -48*time.Hour, -24*time.Hour)

	grant, err := db.GetActiveHHGrant(ctx, "kc-hh-expired")
	require.NoError(t, err)
	assert.Nil(t, grant)
}

func TestGetActiveHHGrant_FutureStart_ReturnsNil(t *testing.T) {
	db, ctx := newTestDB(t)

	insertHHGrant(t, db, ctx, "kc-hh-future", 24*time.Hour, 48*time.Hour)

	grant, err := db.GetActiveHHGrant(ctx, "kc-hh-future")
	require.NoError(t, err)
	assert.Nil(t, grant)
}
