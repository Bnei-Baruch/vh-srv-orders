package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func countsAllOrders() int64 {
	var result int64
	DB.Model(&Order{}).Count(&result)
	return result
}

func countsFilteredOrders(filter string) int64 {
	var result int64
	DB.Model(&Order{}).Where("\"Status\" = ?", filter).Count(&result)
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

	if accountID == 0 {
		return o, errors.New("Null account")
	}

	o.AccountID = accountID

	DB.Create(&o)

	return o, nil
}

func postJSON(method string, url string, payload []byte) (*http.Response, error) {
	fmt.Println("POSTING TO ENDPOINT")
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

func createPendingPayment(sum float32, oid uint, pmx string) (Payment, error) {

	p := Payment{
		Amount:        sum,
		PaymentType:   "pelecard",
		OrderID:       oid,
		PaymentStatus: "pending",
	}

	DB.Create(&p)

	paramx := "mb-" + strconv.FormatUint(uint64(p.ID), 10) + os.Getenv("SUFX") + pmx
	ordkey := "ord-" + strconv.FormatUint(uint64(oid), 10) + os.Getenv("SUFX")
	fmt.Printf(">>>> ParamX: %s\n", paramx)

	p.ParamX = paramx
	p.Ordkey = ordkey
	DB.Model(&p).Updates(p)
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

// Renewal function

//Get Order
func getOrderByID(orderID uint) Order {
	var o Order
	result := DB.Where(&Order{ID: orderID}).First(&o)

	if result.Error != nil {
		log.Printf("\n## ERROR - NO ORDER %v\n", orderID)
	}

	return o
}

//Get Payment
func getPaymentForOrderID(orderID uint) Payment {
	var p Payment
	result := DB.Where(&Payment{OrderID: orderID, PaymentStatus: "success"}).First(&p)

	if result.Error != nil {
		log.Printf("\n## ERROR - NO PAYMENT for ORDER %v\n", orderID)
	}
	return p
}

// Get Account
func getAccountForOrderID(orderID uint) Account {
	var a Account
	o := getOrderByID(orderID)
	result := DB.Where(&Account{ID: o.AccountID}).First(&a)
	if result.Error != nil {
		log.Printf("\n## ERROR - NO ACCOUNT for ORDER %v\n", orderID)
	}
	return a
}

// TODO: REFACTOR
func createRequestPayByToken(a Account, o Order, p Payment, pmx string) (RequestPayment, Payment) {
	newp, _ := createPendingPayment(o.Amount, o.ID, pmx)
	newp.PelecardToken = p.PelecardToken
	newp.AuthNo = p.AuthNo

	extPay := RequestPayment{
		UserKey: newp.Ordkey,

		GoodURL:    "http://ec41a043fda1.ngrok.io/pelecard/good",
		ErrorURL:   "http://ec41a043fda1.ngrok.io/pelecard/error",
		CancelURL:  "http://ec41a043fda1.ngrok.io/pelecard/cancel",
		ApprovalNo: p.AuthNo,
		Token:      p.PelecardToken,

		Name:         a.FirstName + " " + a.LastName,
		Price:        o.Amount,
		Currency:     o.Currency,
		Email:        a.Email,
		Phone:        "+NA",
		Street:       a.Street,
		City:         a.City,
		Country:      "Undef",
		Participans:  "1",
		Details:      "Membership",
		SKU:          "40037",
		VAT:          "f",
		Installments: 1,
		Language:     o.OrderLanguage,
		Reference:    newp.ParamX,
		Organization: "ben2",
	}

	return extPay, newp
}

func renewPaymentByToken(extPay RequestPayment, pmx string) (interface{}, error) {
	payload, _ := json.Marshal(extPay)
	var url string
	if pmx == "t" {
		url = "https://checkout.kbb1.com/token/charge"
	} else if pmx == "e" {
		url = "https://checkout.kbb1.com/emv/charge"
	}
	resp, err := postJSON("POST", url, payload)
	defer resp.Body.Close()
	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	parsableBody := string(body)
	//actualURL := strings.Split(parsableBody, "'")[1]

	fmt.Println("response URL:", parsableBody)
	var i interface{}
	json.Unmarshal(body, &i)
	fmt.Println(i)
	return i, err
}

func renewOrder(orderID uint, pmx string) string {
	/*
			get account by order
			if no token in account
			get payment for order
			extract token
			make payment by token
			if payment successfull (handled in /pelecard good then ... )
			TODO update account with token
			TODO update order

		var a Account

	*/
	o := getOrderByID(orderID)
	p := getPaymentForOrderID(orderID)
	a := getAccountForOrderID(orderID)

	// if a.PaymentToken == "" {
	// 	fmt.Printf("##\nTOKEN IS NULL \n##\n")
	// 	a.PaymentToken = p.PelecardToken
	// 	// add other parameter
	// 	// parse payment card stuff (split and convert to int)
	// 	DB.Model(&a).Uoken(a, o, p, pmx)
	pr, newp := createRequestPayByToken(a, o, p, pmx)
	resp, err := renewPaymentByToken(pr, pmx)
	if err != nil {
		newp.PaymentStatus = "failed"
		newp.Success = "0"
	}
	answers := resp.(map[string]interface{})
	// data := answers["data"].(string)
	// fmt.Println(data)
	if answers["status"].(string) == "success" {
		newp.PaymentStatus = "success"
		newp.Success = "1"
		data := answers["data"].(string)
		fmt.Println(data)
		flagOrderAsRenewed(orderID)
	} else {
		newp.PaymentStatus = "failed"
		newp.Success = "0"
	}
	DB.Model(&newp).Updates(newp)
	updateOrderAfterPayment(newp)
	return newp.Success
}

func flagOrderAsRenewed(orderID uint) {
	req := `update orders 
		set "Flag" = 'renewed'
		where id = ?`

	res := DB.Exec(req, orderID)

	if res.Error != nil {
		fmt.Println(res.Error)
	}

}

func chargeOrdersToRenew(pmx string) int {
	sqlQuery := `
	Select id from orders 
	Where "Status" = 'paid'
	and "Type" = 'recurring'
	and "Flag" = 'torenew'
	`
	rows, err := DB.Raw(sqlQuery).Rows()

	if err != nil {
		return -1
	}
	defer rows.Close()

	var count int
	var id int
	count = 0

	for rows.Next() {
		rows.Scan(&id)
		fmt.Printf(">>> Renewing %d\n", id)
		status := renewOrder(uint(id), pmx)
		//status := "1"
		if status == "1" {
			count++
		} else {
			log.Printf("## Error with %v", id)
		}
	}
	return count
}

func flagOrdersToRenew(month int64, year int64) int64 {
	// fmt.Println(month)
	// fmt.Println(year)
	// return 2

	// Select all unique individuals who have
	// an active renewable order
	qOPotentialStr := `
	select userkey, count(userkey) as qt 
	from orders where ("Status" = 'paid'
	or "Status" = 'nosuccess')
	and "Type" = 'recurring'
	group by userkey 
	order by qt desc
	`
	rows, err := DB.Raw(qOPotentialStr).Rows()

	if err != nil {
		fmt.Println("error 1")
		return -1
	}

	type qOPotential struct {
		Userkey string
		Qt      int64
	}

	var aOPotential qOPotential

	defer rows.Close()

	var counter int64
	counter = 0

	for rows.Next() {
		DB.ScanRows(rows, &aOPotential)

		// fmt.Printf("Key: %s  -- Qt: %d\n",
		// 	aOPotential.Userkey,
		// 	aOPotential.Qt)

		qOSelectStr := `
		select * from orders 
		where userkey = ?
		and ("Status"='paid'
		or "Status"='nosuccess')
		and "Type" = 'recurring'
		order by "PaymentDate" desc
		limit 1
		`
		oselected, err := DB.Raw(qOSelectStr, aOPotential.Userkey).Rows()

		if err != nil {
			fmt.Println("error 2")
			fmt.Println(err)
			return -1
		}

		defer oselected.Close()
		var aOSelect Order

		for oselected.Next() {
			DB.ScanRows(oselected, &aOSelect)

			//fmt.Println(aOSelect.PaymentDate)
			//fmt.Println(int(aOSelect.PaymentDate.Month()))

			if int64(aOSelect.PaymentDate.Month()) == month && int64(aOSelect.PaymentDate.Year()) == year {
				fmt.Printf("No need to charge order %d\n", aOSelect.ID)
			} else {
				fmt.Printf("Mark Order %d for renewal\n", aOSelect.ID)
				flagOrderForRenewal(uint(aOSelect.ID))
				counter++
			}
		}
	}
	return counter
}

func flagOrderForRenewal(id uint) {
	req := `
		update orders
		set "Flag" = 'torenew'
		where id = ?`

	res := DB.Exec(req, id)

	if res.Error != nil {
		fmt.Println(res.Error)
	}

}

func flagDuplicateOrders(ProductType string) int {
	req := `select "AccountID" as id, count(*) as "duplicate" 
from orders where "Status" = 'paid' 
group by "AccountID" 
having count(*) > 1
order by duplicate desc`

	rows, err := DB.Raw(req).Rows() // (*sql.Rows, error)
	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	var b int
	for rows.Next() {
		rows.Scan(&id, &b)
		flagOrdersByAccountID(id, "duplicate")
		count++
	}
	return count
}

func addNoteToOrder(oid uint, note string) {
	o := getOrderByID(uint(oid))
	o.Note = note
	DB.Model(&o).Updates(o)
}
func addFlagToOrder(oid uint, flag string) {
	o := getOrderByID(uint(oid))
	o.Flag = flag
	DB.Model(&o).Updates(o)
}

func flagOrdersByAccountID(aid int, flag string) int {
	req := `select id from orders where "AccountID" = ? and "Status" = 'paid'`
	rows, err := DB.Raw(req, aid).Rows() // (*sql.Rows, error)
	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	for rows.Next() {
		rows.Scan(&id)
		addFlagToOrder(uint(id), flag)
		count++
	}
	return count

}

func countsAllOrdersByMonth(filter string, month string) int64 {
	req := `select id  from orders 
	where "Status" = ? and "Type" = 'recurring' 
	 and date_part('month', "PaymentDate") = ? `
	rows, err := DB.Raw(req, filter, month).Rows()

	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var id int
	count = 0
	for rows.Next() {
		rows.Scan(&id)
		fmt.Println(id)
		count++
	}
	return int64(count)

}

func countsAllOrdersByMonthAndCurrency(filter string, month string, currency string) (int64, float32) {
	req := `select id, "Amount"  from orders 
	where "Status" = ? and "Type" = 'recurring' 
	 and "Currency" = ?
	 and date_part('month', "PaymentDate") = ? `
	rows, err := DB.Raw(req, filter, currency, month).Rows()

	if err != nil {
		return -1, -1
	}

	defer rows.Close()

	var count int
	var id int
	var sum float32
	var amount string
	count = 0
	sum = 0

	for rows.Next() {
		rows.Scan(&id, &amount)
		fmt.Println(id)

		af, _ := strconv.ParseFloat(amount, 32)

		sum = sum + float32(af)
		count++
	}
	return int64(count), sum

}

func getAllOrdersByAccounts(aid uint) []Order {
	//TODO: refactor using ORM functions

	var ordersDuplicate []Order
	DB.Where(&Order{AccountID: aid, Status: "paid"}).Find(&ordersDuplicate)

	return ordersDuplicate

}

func cleanDuplicates(aid uint, month string) {
	// Remove payment
	req1 := `update orders 
	set "Status"='removed'
	where "AccountID"= ? and date_part('month', "PaymentDate") < ?
	`
	DB.Exec(req1, aid, month)

	// Remove duplicate status
	req2 := `update orders 
	set "Flag"=''
	where "AccountID"= ? 
	`
	DB.Exec(req2, aid)

}

// find active orders by Keycloak ID
func activeOrderByKeycloakID(id string) int {
	req := `select o.id 
	from orders as o, accounts as a 
	where a."UserKey" = ? and 
	o."AccountID" = a.id and
	o."Status" = 'paid'  and
    o."ProductType" = 'globalmembership'
`

	rows, err := DB.Raw(req, id).Rows() // (*sql.Rows, error)
	if err != nil {
		return -1
	}
	defer rows.Close()
	var count int
	var oid int
	count = 0
	for rows.Next() {
		rows.Scan(&oid)
		count++
	}
	return count

}
