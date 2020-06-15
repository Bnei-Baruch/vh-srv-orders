package main

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func countsOrders() int64 {
	var result int64
	DB.Model(&Order{}).Count(&result)
	return result
}

func createOrder(req RequestOrder) (Order, error) {
	o := Order{
		Type:          req.Type,
		ProductType:   req.ProductType,
		RecuringFreq:  req.RecurringFreq,
		Organization:  req.Organization,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        "pending",
		OrderLanguage: req.OrderLanguage,
		AccountID:     0,
	}

	a := Account{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Email:     req.Email,
		Phone:     req.Phone,
		Street:    req.Street,
		City:      req.City,
		State:     req.State,
		Postcode:  req.Postcode,
		Country:   req.Country,

		AccountType: "personal",
		UserKey:     req.UserKey,
	}

	accountID := CreateOrUpdateAccount(a)
	o.AccountID = accountID
	DB.Create(&o)

	return o, nil
}

func postJSON(method string, url string, payload []byte) (*http.Response, error) {
	payReq, _ := http.NewRequest(method, url, bytes.NewReader(payload))
	payReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(payReq)
	if err != nil {
		return nil, err
	}
	return resp, err

}

func createPayment(req RequestOrder, o Order) (Payment, error) {

	p := Payment{
		Amount:        req.Amount,
		PaymentType:   "pelecard",
		OrderID:       o.ID,
		PaymentStatus: "pending",
	}

	DB.Create(&p)
	return p, nil

}

func updatePayment(req RequestPaid) (Payment, error) {
	var p Payment

	if len(req.Error) > 0 {
		return p, errors.New(req.Error)
	}

	orderid, err := strconv.ParseUint(strings.Split(req.UserKey, "-")[1], 10, 0)
	if err != nil {
		return p, err
	}
	paymentid, err := strconv.ParseUint(strings.Split(req.ParamX, "-")[1], 10, 0)
	if err != nil {
		return p, err
	}

	// Get payment
	if DB.Where(&Payment{OrderID: uint(orderid), ID: uint(paymentid)}).First(&p).RecordNotFound() {
		return p, errors.New("Cannot find related Order for Payment")
	}

	//update payment object
	if req.Success == "1" {
		p.PaymentStatus = "success"
		p.PaymentType = "pelecard"
		p.ParamX = req.ParamX
		p.AuthNo = req.AuthNo
		p.ConfirmationKey = req.ConfirmationKey
		p.Success = req.Success
		p.PelecardToken = req.Token
		p.TransactionID = req.TransactionID
		p.CCBrand = req.CCBrand
		p.CardHebrewName = req.CardHebrewName
		p.CCAbroadCard = req.CCAbroadCard
		p.CCCompanyClearer = req.CCCompanyClearer
		p.CreditType = req.CreditType
		p.CCExpDate = req.CCExpDate
		p.CCNumber = req.CCNumber
		p.DebitCode = req.DebitCode
		p.DebitCurrency = req.DebitCurrency
		p.DebitTotal = req.DebitTotal
		p.DebitType = req.DebitType
		p.FirstPaymentTotal = req.FirstPaymentTotal
		p.FixedPaymentTotal = req.FixedPaymentTotal
		p.TotalPayments = req.TotalPayments
		p.JParam = req.JParam
		p.TransactionInitTime = req.TransactionInitTime
		p.TransactionUpdateTime = req.TransactionUpdateTime
		p.VoucherID = req.VoucherID
	} else {
		p.PaymentStatus = "failed"
		p.ErrorMsg = "Failed" // TODO: improve
		p.PaymentType = "pelecard"
	}

	DB.Model(&p).Updates(p)
	return p, nil

}

func createOrphanPayment(req RequestPaid) (Payment, error) {

	p := Payment{
		PaymentStatus: "orphan",
	}

	if req.Success == "1" {
		p.PaymentType = "pelecard"
		p.ParamX = req.ParamX
		p.AuthNo = req.AuthNo
		p.ConfirmationKey = req.ConfirmationKey
		p.Success = req.Success
		p.PelecardToken = req.Token
		p.TransactionID = req.TransactionID
		p.CardHebrewName = req.CardHebrewName
		p.CCAbroadCard = req.CCAbroadCard
		p.CCCompanyClearer = req.CCCompanyClearer
		p.CreditType = req.CreditType
		p.CCExpDate = req.CCExpDate
		p.CCNumber = req.CCNumber
		p.DebitCode = req.DebitCode
		p.DebitCurrency = req.DebitCurrency
		p.DebitTotal = req.DebitTotal
		p.DebitType = req.DebitType
		p.FirstPaymentTotal = req.FirstPaymentTotal
		p.JParam = req.TotalPayments
		p.TransactionInitTime = req.TransactionInitTime
		p.TransactionUpdateTime = req.TransactionUpdateTime
		p.VoucherID = req.VoucherID
	} else {
		p.PaymentStatus = "failed"
		p.ErrorMsg = "Failed" // TODO: improve
		p.PaymentType = "pelecard"
	}

	DB.Create(&p)

	return p, nil
}

func updateOrderAfterPayment(p Payment) (Order, error) {
	var o Order

	result := DB.Where("ID = ?", p.OrderID).First(&o)

	if result.Error != nil {
		return o, result.Error
	}

	if p.Success == "1" {
		o.Status = "paid"
		o.PaymentDate = time.Now()
	} else {
		o.Status = "nosuccess"
	}

	DB.Model(&o).Update(o)

	return o, nil
}

func generateOrders() Order {
	o := Order{
		Type:         "recurring",
		ProductType:  "galaxy",
		RecuringFreq: 30,
		AccountID:    1,
		Organization: "bb",
		Amount:       20,
		Currency:     "us",
	}

	return o
}

func generatePayment(o Order) Payment {
	p := Payment{
		Amount:      20,
		PaymentType: "plop",
		OrderID:     o.ID,
	}
	return p
}

func generateInvoice(p Payment) Invoice {
	i := Invoice{
		FirstName: "Paul",
		LastName:  "Jenkins",
		Email:     "Paull.Jenkings@gmail.com",
		Phone:     "+332983945",
		Street:    "Main Street, 145",
		City:      "London",
		State:     "England",
		Postcode:  "W38 7EC",
		Country:   "UK",

		OrderLanguage: "EN",
		PaymentID:     p.ID,
	}
	return i
}
