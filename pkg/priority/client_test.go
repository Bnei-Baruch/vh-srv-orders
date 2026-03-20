package priority

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestGetCustomerByEmail_Success(t *testing.T) {
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
		assert.Equal(t, "1", r.URL.Query().Get("$top"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "CUST001", result.CustName)
	assert.Equal(t, "Test Customer", result.CustDes)
	assert.Equal(t, "test@example.com", result.Email)
}

func TestGetCustomerByEmail_NotFound_Returns404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByEmail(context.Background(), "notfound@example.com")

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetCustomerByEmail_EmptyResult(t *testing.T) {
	response := CustomerODataResponse{
		Value: []Customer{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByEmail(context.Background(), "empty@example.com")

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetCustomerByEmail_NilValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByEmail(context.Background(), "test@example.com")

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestGetCustomerByEmail_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	result, err := client.GetCustomerByEmail(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority API error [500]")
}

func TestGetCustomerByEmail_RequestError(t *testing.T) {
	// Use an invalid URL to trigger a connection error
	client := newTestClient("http://localhost:1") // port 1 is typically closed
	result, err := client.GetCustomerByEmail(context.Background(), "test@example.com")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "priority client request failed")
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
			{FNCNUM: "FNC004", ACCNAME: "40001", DEBIT: 500.0, CODE: "USD", FNCDATE: olderThan12Months},  // Should be excluded (too old)
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
	assert.Equal(t, 50.0, result["ILS"])
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
	assert.Contains(t, err.Error(), "customer not found")
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
	assert.Contains(t, err.Error(), "customer not found")
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
	assert.Contains(t, err.Error(), "c.GetCustomerByEmail")
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
			{FNCNUM: "FNC002", ACCNAME: "40001", DEBIT: 200.0, CODE: "ILS", FNCDATE: validDate},
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
	assert.Equal(t, 250.0, result["USD"])  // 100 + 150
	assert.Equal(t, 200.0, result["ILS"])  // 200
	assert.Equal(t, 125.5, result["EUR"])  // 50 + 75.5
}

