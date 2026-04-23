package billing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ParseReconcileEntries
// ---------------------------------------------------------------------------

func TestParseReconcileEntries_SingleEntry(t *testing.T) {
	line := `time=2026-04-12T03:42:09.000+03:00 level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=100 account_id=10 payment_id=200 amount=90 currency=NIS pricing_version=v2 terminal=token err="tx commit failed"`
	entries, err := ParseReconcileEntries(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, uint(100), e.OrderID)
	assert.Equal(t, 10, e.AccountID)
	assert.Equal(t, 200, e.PaymentID)
	assert.Equal(t, 90.0, e.Amount)
	assert.Equal(t, "NIS", e.Currency)
	assert.Equal(t, "v2", e.PricingVersion)
	assert.Equal(t, "token", e.Terminal)
}

func TestParseReconcileEntries_MultipleEntries(t *testing.T) {
	lines := `time=2026-04-12T03:00:00.000Z level=INFO msg="Starting charge phase"
time=2026-04-12T03:00:01.000Z level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=100 account_id=10 payment_id=200 amount=90 currency=NIS pricing_version=v2 terminal=token err="fail"
time=2026-04-12T03:00:02.000Z level=INFO msg="Order renewed successfully"
time=2026-04-12T03:00:03.000Z level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=101 account_id=11 payment_id=201 amount=20 currency=USD pricing_version=v1 terminal=emv err="timeout"
time=2026-04-12T03:00:04.000Z level=INFO msg="Charge phase completed"`

	entries, err := ParseReconcileEntries(strings.NewReader(lines))
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, uint(100), entries[0].OrderID)
	assert.Equal(t, uint(101), entries[1].OrderID)
	assert.Equal(t, "token", entries[0].Terminal)
	assert.Equal(t, "emv", entries[1].Terminal)
}

func TestParseReconcileEntries_NoMarkers(t *testing.T) {
	lines := `time=2026-04-12T03:00:00.000Z level=INFO msg="Starting charge phase"
time=2026-04-12T03:00:01.000Z level=INFO msg="Order renewed successfully"`

	entries, err := ParseReconcileEntries(strings.NewReader(lines))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestParseReconcileEntries_EmptyInput(t *testing.T) {
	entries, err := ParseReconcileEntries(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestParseReconcileEntries_MissingField(t *testing.T) {
	// Missing payment_id
	line := `time=2026-04-12T03:00:00.000Z level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=100 account_id=10 amount=90 currency=NIS`
	_, err := ParseReconcileEntries(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing payment_id")
}

func TestParseReconcileEntries_InvalidOrderID(t *testing.T) {
	line := `time=2026-04-12T03:00:00.000Z level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=abc account_id=10 payment_id=200 amount=90 currency=NIS`
	_, err := ParseReconcileEntries(strings.NewReader(line))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid order_id")
}

func TestParseReconcileEntries_FloatAmount(t *testing.T) {
	line := `time=2026-04-12T03:00:00.000Z level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=100 account_id=10 payment_id=200 amount=35.5 currency=USD pricing_version=v2 terminal=token err="fail"`
	entries, err := ParseReconcileEntries(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, 35.5, entries[0].Amount)
}

func TestParseReconcileEntries_QuotedErrorField(t *testing.T) {
	line := `time=2026-04-12T03:00:00.000Z level=ERROR msg=CHARGE_SUCCESS_DB_FAIL order_id=100 account_id=10 payment_id=200 amount=90 currency=NIS pricing_version=v2 terminal=token err="post-payment error: tx.Exec [flag renewed]: connection reset"`
	entries, err := ParseReconcileEntries(strings.NewReader(line))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, uint(100), entries[0].OrderID)
}
