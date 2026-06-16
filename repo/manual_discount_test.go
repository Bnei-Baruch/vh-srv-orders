package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// insertActiveDiscount inserts a currently-active manual discount directly via SQL.
func insertActiveDiscount(t *testing.T, db *OrdersDB, ctx context.Context, keycloakID string) int {
	t.Helper()
	var id int
	err := db.QueryRow(ctx,
		`INSERT INTO manual_discount (keycloak_id, start_date, end_date, type, properties)
		 VALUES ($1, NOW() - INTERVAL '1 hour', NOW() + INTERVAL '30 days', 'percent', '{"discount_pct":10}')
		 RETURNING id`,
		keycloakID,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// pctDiscountReq builds a ManualDiscountReq for a percent discount.
func pctDiscountReq(keycloakID string, id *int) ManualDiscountReq {
	pct := 25.0
	props, _ := json.Marshal(ManualDiscountProperties{DiscountPct: &pct})
	return ManualDiscountReq{
		ID:         id,
		KeycloakID: keycloakID,
		Type:       "percent",
		Properties: null.JSONFrom(props),
		StartDate:  time.Now().Add(-time.Hour),
		EndDate:    time.Now().Add(30 * 24 * time.Hour),
	}
}

// ---------------------------------------------------------------------------
// UpsertManualDiscount — insert
// ---------------------------------------------------------------------------

func TestUpsertManualDiscount_Insert_CreatesRecord(t *testing.T) {
	db, ctx := newTestDB(t)

	md, err := db.UpsertManualDiscount(ctx, pctDiscountReq("kc-new", nil))

	require.NoError(t, err)
	assert.Greater(t, md.ID, 0)
	assert.Equal(t, "kc-new", md.KeycloakID)
	assert.Equal(t, "percent", md.Type)
}

func TestUpsertManualDiscount_Insert_CancelsExistingActiveDiscount(t *testing.T) {
	db, ctx := newTestDB(t)
	oldID := insertActiveDiscount(t, db, ctx, "kc-abc")

	_, err := db.UpsertManualDiscount(ctx, pctDiscountReq("kc-abc", nil))

	require.NoError(t, err)
	active, err := db.GetActiveManualDiscount(ctx, "kc-abc")
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.NotEqual(t, oldID, active.ID, "old discount should have been cancelled")
}

// ---------------------------------------------------------------------------
// UpsertManualDiscount — update
// ---------------------------------------------------------------------------

func TestUpsertManualDiscount_Update_ModifiesRecord(t *testing.T) {
	db, ctx := newTestDB(t)
	existingID := insertActiveDiscount(t, db, ctx, "kc-upd")

	pct := 50.0
	props, _ := json.Marshal(ManualDiscountProperties{DiscountPct: &pct})
	req := ManualDiscountReq{
		ID:         &existingID,
		KeycloakID: "kc-upd",
		Type:       "percent",
		Properties: null.JSONFrom(props),
		StartDate:  time.Now().Add(-time.Hour),
		EndDate:    time.Now().Add(60 * 24 * time.Hour),
	}

	md, err := db.UpsertManualDiscount(ctx, req)

	require.NoError(t, err)
	assert.Equal(t, existingID, md.ID)
	assert.Equal(t, "kc-upd", md.KeycloakID)
}

func TestUpsertManualDiscount_Update_WrongKeycloakID_ReturnsErrNoRowsAffected(t *testing.T) {
	db, ctx := newTestDB(t)
	existingID := insertActiveDiscount(t, db, ctx, "kc-owner")

	_, err := db.UpsertManualDiscount(ctx, ManualDiscountReq{
		ID:         &existingID,
		KeycloakID: "kc-other",
		Type:       "percent",
		Properties: null.JSONFrom([]byte(`{"discount_pct":10}`)),
		StartDate:  time.Now().Add(-time.Hour),
		EndDate:    time.Now().Add(30 * 24 * time.Hour),
	})

	assert.ErrorIs(t, err, common.ErrNoRowsAffected)
	// Original record must be untouched — transaction was rolled back.
	active, err := db.GetActiveManualDiscount(ctx, "kc-owner")
	require.NoError(t, err)
	assert.NotNil(t, active)
}

// ---------------------------------------------------------------------------
// CancelManualDiscount
// ---------------------------------------------------------------------------

func TestCancelManualDiscount_SetsEndDateToPast(t *testing.T) {
	db, ctx := newTestDB(t)
	insertActiveDiscount(t, db, ctx, "kc-cancel")

	active, err := db.GetActiveManualDiscount(ctx, "kc-cancel")
	require.NoError(t, err)
	assert.NotNil(t, active, "discount should be active before cancellation")

	err = db.CancelManualDiscount(ctx, "kc-cancel")

	require.NoError(t, err)
	active, err = db.GetActiveManualDiscount(ctx, "kc-cancel")
	require.NoError(t, err)
	assert.Nil(t, active)
}

func TestCancelManualDiscount_NoActiveDiscount_ReturnsErrNoRowsAffected(t *testing.T) {
	db, ctx := newTestDB(t)

	err := db.CancelManualDiscount(ctx, "kc-nobody")

	assert.ErrorIs(t, err, common.ErrNoRowsAffected)
}

// ---------------------------------------------------------------------------
// GetActiveManualDiscount
// ---------------------------------------------------------------------------

func TestGetActiveManualDiscount_UnknownKeycloakID_ReturnsNil(t *testing.T) {
	db, ctx := newTestDB(t)

	md, err := db.GetActiveManualDiscount(ctx, "kc-unknown")

	require.NoError(t, err)
	assert.Nil(t, md)
}

func TestGetActiveManualDiscount_ExpiredDiscount_ReturnsNil(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.Exec(ctx,
		`INSERT INTO manual_discount (keycloak_id, start_date, end_date, type, properties)
		 VALUES ('kc-expired', NOW() - INTERVAL '10 days', NOW() - INTERVAL '1 day', 'percent', '{"discount_pct":10}')`,
	)
	require.NoError(t, err)

	md, err := db.GetActiveManualDiscount(ctx, "kc-expired")

	require.NoError(t, err)
	assert.Nil(t, md)
}

func TestGetActiveManualDiscount_FutureDiscount_ReturnsNil(t *testing.T) {
	db, ctx := newTestDB(t)
	_, err := db.Exec(ctx,
		`INSERT INTO manual_discount (keycloak_id, start_date, end_date, type, properties)
		 VALUES ('kc-future', NOW() + INTERVAL '1 day', NOW() + INTERVAL '30 days', 'percent', '{"discount_pct":10}')`,
	)
	require.NoError(t, err)

	md, err := db.GetActiveManualDiscount(ctx, "kc-future")

	require.NoError(t, err)
	assert.Nil(t, md)
}

// ---------------------------------------------------------------------------
// GetAllManualDiscounts
// ---------------------------------------------------------------------------

func TestGetAllManualDiscounts_NoFilter_ReturnsAll(t *testing.T) {
	db, ctx := newTestDB(t)
	insertActiveDiscount(t, db, ctx, "kc-1")
	insertActiveDiscount(t, db, ctx, "kc-2")

	result, err := db.GetAllManualDiscounts(ctx, "")

	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestGetAllManualDiscounts_ExactFilter_ReturnsMatch(t *testing.T) {
	db, ctx := newTestDB(t)
	insertActiveDiscount(t, db, ctx, "kc-match")
	insertActiveDiscount(t, db, ctx, "kc-other")

	result, err := db.GetAllManualDiscounts(ctx, "kc-match")

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "kc-match", result[0].KeycloakID)
}

func TestGetAllManualDiscounts_ExactFilter_DoesNotMatchSubstring(t *testing.T) {
	db, ctx := newTestDB(t)
	insertActiveDiscount(t, db, ctx, "kc-match")
	insertActiveDiscount(t, db, ctx, "kc-match-extra")

	result, err := db.GetAllManualDiscounts(ctx, "kc-match")

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "kc-match", result[0].KeycloakID)
}

func TestGetAllManualDiscounts_NoMatch_ReturnsEmpty(t *testing.T) {
	db, ctx := newTestDB(t)
	insertActiveDiscount(t, db, ctx, "kc-abc")

	result, err := db.GetAllManualDiscounts(ctx, "kc-xyz")

	require.NoError(t, err)
	assert.Empty(t, result)
}
