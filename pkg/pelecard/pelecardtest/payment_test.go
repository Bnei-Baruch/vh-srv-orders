package pelecardtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.bbdev.team/vh/pay/orders/pkg/pelecard"
)

func paymentClient(t *testing.T, h http.HandlerFunc) *pelecard.Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &pelecard.Client{Client: resty.New(), BaseURL: srv.URL, Tokens: stubTokens{token: "tok"}}
}

// The read names the order in the path, escaped, and the payment in
// ?reference, with the bearer; the answer's keys are external_payments'.
func TestGetPayment_AsksForThePaymentAndReadsTheRecord(t *testing.T) {
	var gotPath, gotRawPath, gotRef, gotAuth string
	c := paymentClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotRawPath, gotRef, gotAuth = r.URL.Path, r.URL.RawPath, r.URL.Query().Get("reference"), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"UserKey":"ord-5","AdditionalDetailsParamX":"m-7","CardToken":"tok-1","CardApprovalNo":"0123456"}`))
	})

	got, err := c.GetPayment(context.Background(), "ord-5/x", "m-7")

	require.NoError(t, err)
	assert.Equal(t, "/vh/payment/ord-5/x", gotPath)
	assert.Equal(t, "/vh/payment/ord-5%2Fx", gotRawPath, "the user key is one path segment")
	assert.Equal(t, "m-7", gotRef)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Equal(t, pelecard.GatewayPayment{UserKey: "ord-5", AdditionalDetailsParamX: "m-7", Token: "tok-1", ApprovalNo: "0123456"}, *got)
}

func TestGetPayment_MapsTheStatuses(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"401 after the retry": {http.StatusUnauthorized, `{}`, pelecard.ErrUnauthorized},
		"404":                 {http.StatusNotFound, `{}`, pelecard.ErrPaymentNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			c := paymentClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.GetPayment(context.Background(), "ord-5", "m-7")
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestGetPayment_RefusesAMalformedRecord(t *testing.T) {
	c := paymentClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"UserKey":`))
	})
	_, err := c.GetPayment(context.Background(), "ord-5", "m-7")
	assert.ErrorContains(t, err, "unmarshal")
}

func TestGetPayment_ServerErrorIsAnError(t *testing.T) {
	c := paymentClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	_, err := c.GetPayment(context.Background(), "ord-5", "m-7")
	require.Error(t, err)
	assert.NotErrorIs(t, err, pelecard.ErrPaymentNotFound)
}
