package billing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

// credentialError alternates the two credential failures, which both count
// towards stopping the run.
func credentialError(i int) error {
	if i%2 == 0 {
		return fmt.Errorf("%w: keycloak unreachable", pelecard.ErrNoCredential)
	}
	return fmt.Errorf("%w: charge gateway HTTP error [401]", pelecard.ErrUnauthorized)
}

func renewalOrder(id uint) resolvedOrder {
	data := testRenewalData()
	data.Order.ID = int(id)
	return resolvedOrder{OrderID: id, Data: data, Price: testV1Price()}
}

// chargeWithin runs chargeResolved with one worker. An unexpected mock call
// fails the test from inside that worker and ends it, which would leave the
// dispatcher blocked; the deadline turns that hang into a quick failure.
func chargeWithin(t *testing.T, service *BillingService, orders []resolvedOrder) (int, error) {
	t.Helper()
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		count, err := service.chargeResolved(context.Background(), orders, 1)
		done <- result{count, err}
	}()
	select {
	case r := <-done:
		return r.count, r.err
	case <-time.After(10 * time.Second):
		t.Fatal("charge phase did not finish; an unexpected call likely failed inside the worker")
		return 0, nil
	}
}

// Ten orders in a row fail on the credential, so the run stops. The orders
// after them are never charged: no payment row, no gateway call, no finalize.
// The mocks fail the test on any call they don't expect.
func TestChargeResolved_StopsAfterConsecutiveCredentialFailures(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)

	var orders []resolvedOrder
	for id := uint(1); id <= 15; id++ {
		orders = append(orders, renewalOrder(id))
	}
	for id := uint(1); id <= 10; id++ {
		expectProcessOrder(mockRepo, mockExecutor, id, pelecard.TokenTerminal, nil, credentialError(int(id)))
	}

	count, err := chargeWithin(t, service, orders)

	require.ErrorIs(t, err, ErrChargeStoppedOnCredential)
	assert.Contains(t, err.Error(), "5 of 15 orders left untouched")
	assert.Zero(t, count)
}

// A credential that works for any order resets the count: nine failures, a
// success, then nine more do not stop the run.
func TestChargeResolved_SuccessResetsTheCredentialCount(t *testing.T) {
	service, mockRepo, mockExecutor := newTestService(t)

	var orders []resolvedOrder
	for id := uint(1); id <= 19; id++ {
		orders = append(orders, renewalOrder(id))
		if id == 10 {
			expectProcessOrder(mockRepo, mockExecutor, id, pelecard.TokenTerminal, gatewaySuccess(), nil)
			continue
		}
		expectProcessOrder(mockRepo, mockExecutor, id, pelecard.TokenTerminal, nil, credentialError(int(id)))
	}

	count, err := chargeWithin(t, service, orders)

	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
