package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDefaultEntryPoint(t *testing.T) {
	initDB("mockdb")
	r := initRouter()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/orders/", nil)
	req.Header.Add("Accept", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fail()
	}
}

func TestDBCreateOrder(t *testing.T) {
	initDB("mockdb")
	o := generateOrders()
	DB.Create(&o)
	if DB.NewRecord(o) {
		t.Fail()
	}
}

func TestDBCreateOrderPaymentInvoice(t *testing.T) {
	initDB("mockdb")
	o := generateOrders()
	DB.Create(&o)
	if DB.NewRecord(o) {
		t.Fail()
	}
	p := generatePayment(o)
	DB.Create(&p)
	if DB.NewRecord(p) {
		t.Fail()
	}
	i := generateInvoice(p)
	DB.Create(&i)
	if DB.NewRecord(i) {
		t.Fail()
	}

}
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	initConf()
	os.Exit(m.Run())
}
