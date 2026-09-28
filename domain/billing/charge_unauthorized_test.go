package billing

import (
	"context"
	"fmt"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/domain/pricing"
	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// A rejected credential fails every order, so the fallback terminal cannot help.
// These pin both halves: the order is not fanned out, and the amount still
// reaches the money totals — without which a run that failed everything reports
// failed_nis=0.

func unauthorizedPrice() *pricing.ChargePrice {
	return &pricing.ChargePrice{
		Amount:         81,
		Currency:       common.CurrencyNIS,
		PricingVersion: "v2",
	}
}

func TestHandleNonRetryableError_UnauthorizedIsNotRetried(t *testing.T) {
	stats := newChargeStats(1)
	err := fmt.Errorf("%w: charge gateway HTTP error [401]", pelecard.ErrUnauthorized)

	handled := handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, unauthorizedPrice(), nil, err)

	require.True(t, handled, "a rejected credential must not fall through to the other terminal")
	assert.Equal(t, int64(1), stats.errorCount.Get("unauthorized"))
}

func TestHandleNonRetryableError_UnauthorizedRecordsTheUncollectedAmount(t *testing.T) {
	stats := newChargeStats(1)
	price := unauthorizedPrice()
	err := fmt.Errorf("%w: charge gateway HTTP error [401]", pelecard.ErrUnauthorized)

	handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, price, nil, err)

	assert.Equal(t, price.Amount, stats.failedSum.Get(common.CurrencyNIS),
		"the amount is uncollected, so it belongs in failed_nis")
	assert.Equal(t, price.Amount, stats.versionFailedSum.Get("v2:"+common.CurrencyNIS),
		"and in the per-version total the summary reports")
	assert.Equal(t, price.Amount, stats.reasonFailedSum.Get("unauthorized:"+common.CurrencyNIS),
		"under a reason of its own, which is the key the summary log reads")

	assert.Zero(t, stats.reasonFailedSum.Get("gateway:"+common.CurrencyNIS),
		"and not under gateway, which is a different incident")
	assert.Zero(t, stats.successSum.Get(common.CurrencyNIS))
}

// The order passes this branch at most once; counting twice would double the
// month's reported shortfall.
func TestHandleNonRetryableError_UnauthorizedCountsOncePerOrder(t *testing.T) {
	stats := newChargeStats(1)
	price := unauthorizedPrice()
	err := fmt.Errorf("%w: charge gateway HTTP error [401]", pelecard.ErrUnauthorized)

	handled := handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, price, nil, err)
	require.True(t, handled)

	assert.Equal(t, int64(1), stats.errorCount.Get("unauthorized"))
	assert.Equal(t, price.Amount, stats.failedSum.Get(common.CurrencyNIS))
}

// A gateway failure is terminal-specific: it must still fall through so the
// other terminal gets its turn, and it must not be counted as unauthorized.
func TestHandleNonRetryableError_GatewayErrorStillFallsThrough(t *testing.T) {
	stats := newChargeStats(1)

	handled := handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, unauthorizedPrice(), nil, fmt.Errorf("charge gateway HTTP error [502]"))

	require.False(t, handled, "the EMV leg is the point of the fallback")
	assert.Zero(t, stats.errorCount.Get("unauthorized"))
	assert.Zero(t, stats.failedSum.Get(common.CurrencyNIS),
		"the terminal branch records this one, after both terminals have been tried")
}

// The other leg shares the same Keycloak client, so it would fail identically
// and write a second pending payment row for nothing.
func TestHandleNonRetryableError_NoCredentialIsNotRetried(t *testing.T) {
	stats := newChargeStats(1)
	price := unauthorizedPrice()
	err := fmt.Errorf("%w: keycloak unreachable", pelecard.ErrNoCredential)

	handled := handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, price, nil, err)

	require.True(t, handled, "the other terminal cannot obtain a credential either")
	assert.Equal(t, int64(1), stats.errorCount.Get("no_credential"))
	assert.Equal(t, price.Amount, stats.reasonFailedSum.Get("no_credential:"+common.CurrencyNIS))
	assert.Equal(t, price.Amount, stats.failedSum.Get(common.CurrencyNIS))
}

// The two stay apart in the summary: misconfiguration versus Keycloak down.
func TestHandleNonRetryableError_CredentialReasonsAreDistinct(t *testing.T) {
	stats := newChargeStats(2)
	price := unauthorizedPrice()

	handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, price, nil,
		fmt.Errorf("%w: charge gateway HTTP error [401]", pelecard.ErrUnauthorized))
	handleNonRetryableError(context.Background(), sentry.CurrentHub(), &stats,
		pelecard.TokenTerminal.Name, price, nil,
		fmt.Errorf("%w: keycloak unreachable", pelecard.ErrNoCredential))

	assert.Equal(t, int64(1), stats.errorCount.Get("unauthorized"))
	assert.Equal(t, int64(1), stats.errorCount.Get("no_credential"))
	assert.Equal(t, price.Amount*2, stats.failedSum.Get(common.CurrencyNIS),
		"both amounts are uncollected and both belong in the total")
}
