package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/volatiletech/null/v9"

	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

func skipUpload(buffer []byte, fileName string) (string, error) {
	return "http://mock.url", nil
}

func Test_offline_payment(t *testing.T) {
	a := NewTestApp(t)
	defer CloseTestApp(a)

	// Mock upload.
	realUpload := utils.UploadFileToS3
	t.Cleanup(func() { utils.UploadFileToS3 = realUpload })
	utils.UploadFileToS3 = skipUpload

	accountReq := repo.Account{UserKey: null.StringFrom(USER_KEY)}
	got := POST(t, a, "/v2/account/", accountReq, http.StatusCreated)
	fmt.Printf("Added account: %+v\n", got)

	offline_payment := repo.OfflinePaymentRequest{
		KeycloakID:    null.StringFrom(USER_KEY),
		Currency:      null.StringFrom("usd"),
		PaymentMethod: null.StringFrom("offline"),
		PaymentDate:   null.TimeFrom(time.Now()),
		Quantity:      1,
		Amount:        15.0,
	}
	got = POST_ROOT(t, a, "/v2/order/offline", offline_payment, http.StatusOK)
	fmt.Printf("Offline payment posted: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))

	got = GET(t, a, "/v2/payments", http.StatusOK)
	fmt.Printf("GET payments: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
	payments := got["data"].([]interface{})
	assert.Equal(t, 1, len(payments))
	fmt.Printf("GET payment: %+v\n", payments[0])
	p := payments[0].(map[string]interface{})
	paymentID := int(p["ID"].(float64))
	assert.Equal(t, "success", (p)["PaymentStatus"].(string))

	update := repo.PaymentUpdate{
		PaymentID:   null.IntFrom(paymentID),
		PaymentType: null.StringFrom("offline"),
		Amount:      null.Float64From(float64(16)), // This is actually NOT expected to change!
		Status:      null.StringFrom("some new status"),
	}

	receipt := AttachedFile{Field: "receipt", Filename: "receipt.pdf", Bytes: []byte("fake content")}
	got = PATCH_ROOT_MULTIPART(t, a, "/v2/payment/", update, []AttachedFile{receipt}, http.StatusOK)
	fmt.Printf("Added account: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))

	got = GET(t, a, fmt.Sprintf("/v2/payment/%d", paymentID), http.StatusOK)
	fmt.Printf("Get payment: %+v\n", got)
	assert.Equal(t, true, got["success"].(bool))
	// Amount is NOT expected to change, we should ignore it's update value.
	assert.Equal(t, float64(15), got["data"].(map[string]interface{})["Amount"].(float64))
	assert.Equal(t, "some new status", got["data"].(map[string]interface{})["PaymentStatus"].(string))
}
