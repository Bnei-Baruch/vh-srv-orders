package priority

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates a client with a custom base URL for testing
func newTestClient(baseURL string) *Client {
	client := resty.New()
	client.SetBaseURL(baseURL)
	client.SetHeaders(map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	})
	return &Client{client: client}
}

func TestGetCustomersByEmail_Success(t *testing.T) {
	customer := Customer{
		CustName: "CUST001",
		CustDes:  "Test Customer",
		Email:    "test@example.com",
	}
	response := CustomerODataResponse{
		Value: []Customer{customer},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/CUSTOMERS", r.URL.Path)
		assert.Equal(t, "EMAIL eq 'test@example.com'", r.URL.Query().Get("$filter"))
		assert.Empty(t, r.URL.Query().Get("$top"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomersByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "CUST001", result[0].CustName)
	assert.Equal(t, "Test Customer", result[0].CustDes)
	assert.Equal(t, "test@example.com", result[0].Email)
}

func TestGetCustomersByEmail_MultipleCustomers(t *testing.T) {
	response := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", CustDes: "First Customer", Email: "shared@example.com"},
			{CustName: "CUST002", CustDes: "Second Customer", Email: "shared@example.com"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomersByEmail(context.Background(), "shared@example.com")

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "CUST001", result[0].CustName)
	assert.Equal(t, "CUST002", result[1].CustName)
}

func TestGetCustomersByEmail_NotFound_Returns404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomersByEmail(context.Background(), "notfound@example.com")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetCustomersByEmail_EmptyResult(t *testing.T) {
	response := CustomerODataResponse{
		Value: []Customer{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomersByEmail(context.Background(), "empty@example.com")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetCustomersByEmail_NilValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomersByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetCustomersByEmail_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomersByEmail(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority API error [500]")
}

func TestGetCustomersByEmail_RequestError(t *testing.T) {
	client := newTestClient("http://localhost:1") // port 1 is typically closed
	result, err := client.GetCustomersByEmail(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority client request failed")
}

func TestGetCustomerByID_Success(t *testing.T) {
	customer := Customer{
		CustName: "CUST001",
		CustDes:  "Test Customer",
		Email:    "test@example.com",
		StatDes:  "פעיל",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/CUSTOMERS('CUST001')", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customer)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByID(context.Background(), "CUST001")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "CUST001", result.CustName)
	assert.Equal(t, "Test Customer", result.CustDes)
	assert.Equal(t, "test@example.com", result.Email)
}

func TestGetCustomerByID_NotFound_ReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByID(context.Background(), "NOTFOUND")

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetCustomerByID_EmptyBodyReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByID(context.Background(), "CUST001")

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetCustomerByID_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByID(context.Background(), "CUST001")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority API error [500]")
}

func TestGetCustomerByID_RequestError(t *testing.T) {
	client := newTestClient("http://localhost:1") // port 1 is typically closed
	result, err := client.GetCustomerByID(context.Background(), "CUST001")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority client request failed")
}

func TestGetActiveCustomersByEmail_FiltersInactiveFlag(t *testing.T) {
	inactive := "Y"
	response := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", Email: "test@example.com", InactiveFlag: &inactive},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetActiveCustomersByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetActiveCustomersByEmail_FiltersStatDes(t *testing.T) {
	response := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", Email: "test@example.com", StatDes: "לא פעיל"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetActiveCustomersByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetActiveCustomersByEmail_MixedReturnsOnlyActive(t *testing.T) {
	inactive := "Y"
	response := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", Email: "test@example.com"},
			{CustName: "CUST002", Email: "test@example.com", InactiveFlag: &inactive},
			{CustName: "CUST003", Email: "test@example.com", StatDes: "לא פעיל"},
			{CustName: "CUST004", Email: "test@example.com", StatDes: "פעיל"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetActiveCustomersByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "CUST001", result[0].CustName)
	assert.Equal(t, "CUST004", result[1].CustName)
}

func TestGetAccountReceivables_Success(t *testing.T) {
	items := []AccountReceivableItem{
		{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: "2025-01-15T00:00:00Z"},
		{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: "ILS", FNCDATE: "2025-02-20T00:00:00Z"},
	}
	response := AccountReceivableODataResponse{
		Value: items,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/ACCOUNTS_RECEIVABLE('CUST001')/ACCFNCITEMS2_SUBFORM", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "CUST001")

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "FNC001", result[0].FNCNUM)
	assert.Equal(t, "FNC002", result[1].FNCNUM)
}

func TestGetAccountReceivables_NotFound_ReturnsEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "NOTFOUND")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAccountReceivables_EmptyValue(t *testing.T) {
	response := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "CUST001")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAccountReceivables_NilValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "CUST001")

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestGetAccountReceivables_Pagination(t *testing.T) {
	requestCount := 0
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if requestCount == 0 {
			// First page with nextLink (must be a full URL)
			response := AccountReceivableODataResponse{
				Value:         []AccountReceivableItem{{FNCNUM: "FNC001"}, {FNCNUM: "FNC002"}},
				ODataNextLink: serverURL + "/nextpage",
			}
			requestCount++
			json.NewEncoder(w).Encode(response)
		} else {
			// Second page (final)
			response := AccountReceivableODataResponse{
				Value: []AccountReceivableItem{{FNCNUM: "FNC003"}},
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "CUST001")

	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "FNC001", result[0].FNCNUM)
	assert.Equal(t, "FNC002", result[1].FNCNUM)
	assert.Equal(t, "FNC003", result[2].FNCNUM)
}

func TestGetAccountReceivables_PaginationError_ReturnsPartialResults(t *testing.T) {
	requestCount := 0
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if requestCount == 0 {
			// First page with nextLink (must be a full URL with scheme)
			response := AccountReceivableODataResponse{
				Value:         []AccountReceivableItem{{FNCNUM: "FNC001"}, {FNCNUM: "FNC002"}},
				ODataNextLink: serverURL + "/nextpage",
			}
			requestCount++
			json.NewEncoder(w).Encode(response)
		} else {
			// Second page returns error
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "CUST001")

	// Should return partial results with error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returning partial results")
	require.Len(t, result, 2)
}

func TestGetAccountReceivables_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetAccountReceivables(context.Background(), "CUST001")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority API error [500]")
}

func TestGetLastContributions_Success(t *testing.T) {
	now := time.Now()
	withinLast12Months := now.AddDate(0, -6, 0).Format(time.RFC3339) // 6 months ago
	olderThan12Months := now.AddDate(0, -14, 0).Format(time.RFC3339) // 14 months ago

	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: withinLast12Months},
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: "USD", FNCDATE: withinLast12Months},
			{FNCNUM: "FNC003", ACCNAME: "40001", DEBIT: 50.0, CODE: "ILS", FNCDATE: withinLast12Months},
			{FNCNUM: "FNC004", ACCNAME: "40001", DEBIT: 500.0, CODE: "USD", FNCDATE: olderThan12Months},   // Should be excluded (too old)
			{FNCNUM: "FNC005", ACCNAME: "50001", DEBIT: 1000.0, CODE: "USD", FNCDATE: withinLast12Months}, // Should be excluded (wrong ACCNAME)
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 300.0, result["USD"]) // 100 + 200, excludes the old one and wrong ACCNAME
	assert.Equal(t, 50.0, result["NIS"])  // ILS normalized to NIS
}

func TestGetLastContributions_MultipleActiveCustomers(t *testing.T) {
	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	customerResponse := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", Email: "shared@example.com"},
			{CustName: "CUST002", Email: "shared@example.com"},
		},
	}

	receivables1 := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: validDate},
		},
	}
	receivables2 := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC003", ACCNAME: "40001", DEBIT: 50.0, CODE: "ILS", FNCDATE: validDate},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/CUSTOMERS":
			json.NewEncoder(w).Encode(customerResponse)
		case "/ACCOUNTS_RECEIVABLE('CUST001')/ACCFNCITEMS2_SUBFORM":
			json.NewEncoder(w).Encode(receivables1)
		case "/ACCOUNTS_RECEIVABLE('CUST002')/ACCFNCITEMS2_SUBFORM":
			json.NewEncoder(w).Encode(receivables2)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "shared@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 300.0, result["USD"]) // 100 + 200
	assert.Equal(t, 50.0, result["NIS"])  // ILS normalized to NIS
}

func TestGetLastContributions_InactiveCustomerSkipped(t *testing.T) {
	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)
	inactive := "Y"

	customerResponse := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", Email: "test@example.com"},
			{CustName: "CUST002", Email: "test@example.com", InactiveFlag: &inactive},
		},
	}

	receivables1 := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 150.0, CODE: "ILS", FNCDATE: validDate},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/CUSTOMERS":
			json.NewEncoder(w).Encode(customerResponse)
		case "/ACCOUNTS_RECEIVABLE('CUST001')/ACCFNCITEMS2_SUBFORM":
			json.NewEncoder(w).Encode(receivables1)
		default:
			// CUST002 receivables should not be requested
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 150.0, result["NIS"]) // ILS normalized to NIS
	assert.NotContains(t, result, "USD")
}

func TestGetLastContributions_NoActiveCustomers(t *testing.T) {
	inactive := "Y"
	customerResponse := CustomerODataResponse{
		Value: []Customer{
			{CustName: "CUST001", Email: "test@example.com", InactiveFlag: &inactive},
			{CustName: "CUST002", Email: "test@example.com", StatDes: "לא פעיל"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customerResponse)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no active customers found")
}

func TestGetLastContributions_CustomerNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CustomerODataResponse{Value: []Customer{}})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "notfound@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no active customers found")
}

func TestGetLastContributions_CustomerEmptyCustName(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "", Email: "test@example.com"}}, // Empty CustName
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customerResponse)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no active customers found")
}

func TestGetLastContributions_GetCustomerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "c.GetActiveCustomersByEmail")
}

func TestGetLastContributions_GetReceivablesError(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestCount == 0 {
			// First request: customer lookup succeeds
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(customerResponse)
			requestCount++
		} else {
			// Second request: receivables fails
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "c.GetAccountReceivables")
}

func TestGetLastContributions_InvalidDateSkipped(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: "USD", FNCDATE: "invalid-date"}, // Should be skipped
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 100.0, result["USD"]) // Only valid date item
}

func TestGetLastContributions_NoContributions(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{}, // No receivables
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result)
}

func TestGetLastContributions_OnlyWrongACCNAME(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "50001", DEBIT: 100.0, CODE: "USD", FNCDATE: validDate}, // Wrong ACCNAME
			{FNCNUM: "FNC002", ACCNAME: "60001", DEBIT: 200.0, CODE: "USD", FNCDATE: validDate}, // Wrong ACCNAME
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result) // All filtered out by ACCNAME
}

func TestGetLastContributions_AllValidACCNAMEs(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 10.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC002", ACCNAME: "40002", DEBIT: 20.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC003", ACCNAME: "40004", DEBIT: 30.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC004", ACCNAME: "40038", DEBIT: 40.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC005", ACCNAME: "40049", DEBIT: 50.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC006", ACCNAME: "40050", DEBIT: 60.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC007", ACCNAME: "40053", DEBIT: 70.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC008", ACCNAME: "40054", DEBIT: 80.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC009", ACCNAME: "40061", DEBIT: 90.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC010", ACCNAME: "40100", DEBIT: 100.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC011", ACCNAME: "99999", DEBIT: 999.0, CODE: "USD", FNCDATE: validDate}, // unknown — excluded
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 550.0, result["USD"]) // 10+20+30+40+50+60+70+80+90+100, excludes 99999
}

func TestGetLastContributions_BoundaryDateExactly12MonthsAgo(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	// AddDate(0, -12, -1) = 12 months and 1 day ago (older than 12 months cutoff)
	olderThan12Months := now.AddDate(0, -12, -1).Format(time.RFC3339)
	// AddDate(0, -11, 0) = 11 months ago (clearly within 12 months)
	elevenMonthsAgo := now.AddDate(0, -11, 0).Format(time.RFC3339)
	// AddDate(0, -6, 0) = 6 months ago (clearly within 12 months)
	sixMonthsAgo := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: olderThan12Months}, // Should be excluded
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: "USD", FNCDATE: elevenMonthsAgo},   // Should be included
			{FNCNUM: "FNC003", ACCNAME: "40001", DEBIT: 50.0, CODE: "USD", FNCDATE: sixMonthsAgo},       // Should be included
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	// The code uses: fncDate.Before(twelveMonthsAgo) to exclude old items
	// So only 11 months ago (200) and 6 months ago (50) should be included
	assert.Equal(t, 250.0, result["USD"]) // 200 + 50
}

func TestGetLastContributions_MultipleCurrencies(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: `ש"ח`, FNCDATE: validDate},
			{FNCNUM: "FNC003", ACCNAME: "40001", DEBIT: 50.0, CODE: "EUR", FNCDATE: validDate},
			{FNCNUM: "FNC004", ACCNAME: "40001", DEBIT: 150.0, CODE: "USD", FNCDATE: validDate},
			{FNCNUM: "FNC005", ACCNAME: "40001", DEBIT: 75.5, CODE: "EUR", FNCDATE: validDate},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 250.0, result["USD"]) // 100 + 150
	assert.Equal(t, 200.0, result["NIS"]) // ש"ח normalized to NIS
	assert.Equal(t, 125.5, result["EUR"]) // 50 + 75.5
}

func TestGetLastContributions_HebrewShekelNormalizedToNIS(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 100.0, CODE: `ש"ח`, FNCDATE: validDate},
			{FNCNUM: "FNC002", ACCNAME: "40002", DEBIT: 54.0, CODE: `ש"ח`, FNCDATE: validDate},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 154.0, result["NIS"])
	assert.NotContains(t, result, `ש"ח`, "raw Hebrew code must not leak to callers")
}

func TestGetLastContributions_UnknownCodeTreatedAsNIS(t *testing.T) {
	customerResponse := CustomerODataResponse{
		Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
	}

	now := time.Now()
	validDate := now.AddDate(0, -6, 0).Format(time.RFC3339)

	receivablesResponse := AccountReceivableODataResponse{
		Value: []AccountReceivableItem{
			{FNCNUM: "FNC001", ACCNAME: "40001", DEBIT: 200.0, CODE: "GBP", FNCDATE: validDate}, // unknown
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 100.0, CODE: `ש"ח`, FNCDATE: validDate},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(customerResponse)
		} else {
			json.NewEncoder(w).Encode(receivablesResponse)
		}
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributions(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	// Unknown GBP falls back to NIS (Priority's internal currency), sums with ש"ח.
	assert.Equal(t, 300.0, result["NIS"])
	assert.NotContains(t, result, "GBP")
}

// --- GetLastContributionsBatch ---

func TestGetLastContributionsBatch_SingleEmail_Success(t *testing.T) {
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: validDate},
				}},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"USD": 100.0}, result.SumGroup([]string{"test@example.com"}))
	assert.Equal(t, 2, result.Stats.Requests)
}

func TestGetLastContributionsBatch_DedupSharedCustomerAcrossEmails(t *testing.T) {
	// Two emails (e.g. spouse aliases) resolving to the SAME Priority customer must only
	// be counted once when grouped together.
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{
					{CustName: "CUST001", Email: "a@x.com"},
					{CustName: "CUST001", Email: "b@x.com"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 500.0, CODE: "NIS", FNCDATE: validDate},
				}},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"a@x.com", "b@x.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"NIS": 500.0}, result.SumGroup([]string{"a@x.com", "b@x.com"}),
		"customer matched by both emails must be counted once, not twice")
}

func TestGetLastContributionsBatch_DistinctCustomersSum(t *testing.T) {
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{
					{CustName: "CUST001", Email: "a@x.com"},
					{CustName: "CUST002", Email: "b@x.com"},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 100.0, CODE: "NIS", FNCDATE: validDate},
				}},
				{ACCNAME: "CUST002", Items: []AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 200.0, CODE: "NIS", FNCDATE: validDate},
				}},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"a@x.com", "b@x.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"NIS": 300.0}, result.SumGroup([]string{"a@x.com", "b@x.com"}))
}

func TestGetLastContributionsBatch_NoActiveCustomer_EmptyResultNoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CustomerODataResponse{Value: []Customer{}})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"unknown@x.com"})

	require.NoError(t, err)
	assert.Empty(t, result.SumGroup([]string{"unknown@x.com"}))
	assert.Equal(t, 1, result.Stats.Requests, "should not call ACCOUNTS_RECEIVABLE when no customer resolved")
}

func TestGetLastContributionsBatch_UnconditionalDebitSum_IncludesNegative(t *testing.T) {
	// Regression: batch's $filter must not exclude DEBIT<=0 rows -- GetLastContributions
	// sums every matching row unconditionally, including negative-DEBIT reversal rows.
	validDate := time.Now().AddDate(0, -3, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 1000.0, CODE: "NIS", FNCDATE: validDate},
					{ACCNAME: "40001", DEBIT: -300.0, CODE: "NIS", FNCDATE: validDate}, // reversal
				}},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"NIS": 700.0}, result.SumGroup([]string{"test@example.com"}))
}

func TestGetLastContributionsBatch_BoundaryDateExactly12MonthsAgo(t *testing.T) {
	// Mirrors TestGetLastContributions_BoundaryDateExactly12MonthsAgo exactly: batch must
	// apply the same precise-instant cutoff as the legacy fetch, not a day-rounded one.
	now := time.Now()
	olderThan12Months := now.AddDate(0, -12, -1).Format(time.RFC3339)
	elevenMonthsAgo := now.AddDate(0, -11, 0).Format(time.RFC3339)
	sixMonthsAgo := now.AddDate(0, -6, 0).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{
					{ACCNAME: "40001", DEBIT: 100.0, CODE: "USD", FNCDATE: olderThan12Months}, // excluded
					{ACCNAME: "40001", DEBIT: 200.0, CODE: "USD", FNCDATE: elevenMonthsAgo},   // included
					{ACCNAME: "40001", DEBIT: 50.0, CODE: "USD", FNCDATE: sixMonthsAgo},       // included
				}},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"USD": 250.0}, result.SumGroup([]string{"test@example.com"}))
}

func TestGetLastContributionsBatch_CustomersPagination(t *testing.T) {
	// Regression: the CUSTOMERS lookup must follow @odata.nextLink like the other paginated
	// calls, or customers beyond the first page silently go missing.
	requestCount := 0
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "CUSTOMERS") {
			if requestCount == 0 {
				requestCount++
				json.NewEncoder(w).Encode(CustomerODataResponse{
					Value:         []Customer{{CustName: "CUST001", Email: "a@x.com"}},
					ODataNextLink: serverURL + "/CUSTOMERS/nextpage",
				})
				return
			}
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{{CustName: "CUST002", Email: "b@x.com"}},
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{{ACCNAME: "40001", DEBIT: 10, CODE: "NIS", FNCDATE: time.Now().Format(time.RFC3339)}}},
				{ACCNAME: "CUST002", Items: []AccountReceivableItem{{ACCNAME: "40001", DEBIT: 20, CODE: "NIS", FNCDATE: time.Now().Format(time.RFC3339)}}},
			},
		})
	}))
	defer server.Close()
	serverURL = server.URL

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"a@x.com", "b@x.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"NIS": 30}, result.SumGroup([]string{"a@x.com", "b@x.com"}))
}

func TestGetLastContributionsBatch_AccountsReceivablePagination(t *testing.T) {
	requestCount := 0
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/CUSTOMERS" {
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
			})
			return
		}
		if requestCount == 0 {
			requestCount++
			json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
				Value: []accountsReceivableExpandItem{
					{ACCNAME: "CUST001", Items: []AccountReceivableItem{{ACCNAME: "40001", DEBIT: 10, CODE: "NIS", FNCDATE: time.Now().Format(time.RFC3339)}}},
				},
				ODataNextLink: serverURL + "/ACCOUNTS_RECEIVABLE/nextpage",
			})
			return
		}
		json.NewEncoder(w).Encode(accountsReceivableExpandResponse{
			Value: []accountsReceivableExpandItem{
				{ACCNAME: "CUST001", Items: []AccountReceivableItem{{ACCNAME: "40001", DEBIT: 20, CODE: "NIS", FNCDATE: time.Now().Format(time.RFC3339)}}},
			},
		})
	}))
	defer server.Close()
	serverURL = server.URL

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"NIS": 30}, result.SumGroup([]string{"test@example.com"}))
}

func TestGetLastContributionsBatch_CustomersAPIError_Propagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	_, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.Error(t, err)
}

func TestGetLastContributionsBatch_AccountsReceivableAPIError_Propagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/CUSTOMERS" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(CustomerODataResponse{
				Value: []Customer{{CustName: "CUST001", Email: "test@example.com"}},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	_, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.Error(t, err)
}

func TestGetLastContributionsBatch_InactiveCustomerExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		inactive := "Y"
		json.NewEncoder(w).Encode(CustomerODataResponse{
			Value: []Customer{{CustName: "CUST001", Email: "test@example.com", InactiveFlag: &inactive}},
		})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{"test@example.com"})

	require.NoError(t, err)
	assert.Empty(t, result.SumGroup([]string{"test@example.com"}))
}

func TestGetLastContributionsBatch_EmptyEmails_NoRequests(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetLastContributionsBatch(context.Background(), []string{})

	require.NoError(t, err)
	assert.Empty(t, result.ByCustomer)
	assert.Equal(t, 0, requests)
}

// --- helpers: buildOrFilter, chunkStrings, sortedKeys ---

func TestBuildOrFilter_Basic(t *testing.T) {
	assert.Equal(t, `EMAIL eq 'a@x.com' or EMAIL eq 'b@x.com'`, buildOrFilter("EMAIL", []string{"a@x.com", "b@x.com"}))
}

func TestBuildOrFilter_SingleValue(t *testing.T) {
	assert.Equal(t, `EMAIL eq 'a@x.com'`, buildOrFilter("EMAIL", []string{"a@x.com"}))
}

func TestBuildOrFilter_EscapesSingleQuote(t *testing.T) {
	assert.Equal(t, `EMAIL eq 'o''brien@x.com'`, buildOrFilter("EMAIL", []string{"o'brien@x.com"}))
}

func TestChunkStrings_EvenChunks(t *testing.T) {
	chunks := chunkStrings([]string{"a", "b", "c", "d"}, 2)
	assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}}, chunks)
}

func TestChunkStrings_UnevenRemainder(t *testing.T) {
	chunks := chunkStrings([]string{"a", "b", "c"}, 2)
	assert.Equal(t, [][]string{{"a", "b"}, {"c"}}, chunks)
}

func TestChunkStrings_SizeGreaterThanLength(t *testing.T) {
	chunks := chunkStrings([]string{"a", "b"}, 10)
	assert.Equal(t, [][]string{{"a", "b"}}, chunks)
}

func TestChunkStrings_EmptyInput_ReturnsNoChunks(t *testing.T) {
	assert.Empty(t, chunkStrings([]string{}, 40))
}

func TestChunkStrings_ZeroSize_ReturnsSingleChunk(t *testing.T) {
	chunks := chunkStrings([]string{"a", "b"}, 0)
	assert.Equal(t, [][]string{{"a", "b"}}, chunks)
}

func TestSortedKeys_ReturnsDeterministicOrder(t *testing.T) {
	set := map[string]struct{}{"c": {}, "a": {}, "b": {}}
	assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(set))
}
