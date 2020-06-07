package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brianvoe/gofakeit/v5"
)

func TestPingAccounts(t *testing.T) {
	r := initRouter()           // get router
	w := httptest.NewRecorder() // response recorder
	req, _ := http.NewRequest("GET", "/accounts/ping", nil)
	req.Header.Add("Accept", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fail()
	}
	expected := `{"ping":"pong"}`
	if w.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			w.Body.String(), expected)
	}
}

func TestCreateAccounts(t *testing.T) {
	r := initRouter()           // Setup router
	w := httptest.NewRecorder() // response recorder

	//Init the DB and count how many records
	originalRecords := countAccounts()
	expectedRecords := originalRecords + 1

	//Generate a fake account to submit
	var a Account
	gofakeit.Struct(&a)

	b, err := json.Marshal(a)
	if err != nil {
		t.Log(err)
	}

	//Submit it
	req, _ := http.NewRequest("POST", "/accounts/new", bytes.NewReader(b))
	req.Header.Add("Accept", "application/json")
	r.ServeHTTP(w, req)

	//if req not working = end
	if w.Code != http.StatusOK {
		t.Fail()
	}
	newRecords := countAccounts()
	if newRecords != expectedRecords {
		t.Errorf("Adding record returned unexpected result: got %v - want %v",
			newRecords, expectedRecords)
	}

}

func TestCount(t *testing.T) {
	actualRecord := countAccounts()
	alsoRecord := countAccounts()
	if alsoRecord != actualRecord {
		t.Errorf("handler returned unexpected body: got %v want %v",
			actualRecord, alsoRecord)
	}
}

// // This function is used to do setup before executing the test functions
// func TestMain(m *testing.M) {
// 	//Set Gin to Test Mode
// 	gin.SetMode(gin.TestMode)
// 	initConf()
// 	initDB()
// 	// Run the other tests
// 	os.Exit(m.Run())
// }
